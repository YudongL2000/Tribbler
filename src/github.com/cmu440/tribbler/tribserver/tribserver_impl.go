package tribserver

import (
	"container/heap"
	// "errors"
	"github.com/cmu440/tribbler/libstore"
	// "math"
	// "log"
	"encoding/json"
	"github.com/cmu440/tribbler/rpc/tribrpc"
	"net"
	"net/rpc"
	"strconv"
	"strings"
	"sync"
	"time"
	// "fmt"
	"net/http"
)

//*****************************Implementation of a Priority Queue*******************
type pqElem struct {
	index     int
	timeStamp time.Time
	source    int
	user      string
	key       string
}

type Queue []*pqElem

func (pq Queue) Len() int {
	return len(pq)
}

func (pq Queue) Less(i, j int) bool {
	// return pq[i].timeStamp > pq[j].timeStamp
	return pq[i].timeStamp.After(pq[j].timeStamp)
}

func (pq Queue) Swap(i, j int) {
	pq[i], pq[j] = pq[j], pq[i]
	pq[i].index = i
	pq[j].index = j
}

func (pq *Queue) Push(x interface{}) {
	n := len(*pq)
	item := x.(*pqElem)
	item.index = n
	*pq = append(*pq, item)
}

func (pq *Queue) Pop() interface{} {
	current := *pq
	n := len(current)
	item := current[n-1]
	item.index = -1
	current[n-1] = nil
	*pq = current[0 : n-1]
	return item
}

func (pq *Queue) update(item *pqElem, userID string, timeStamp time.Time) {
	item.user = userID
	item.timeStamp = timeStamp
	heap.Fix(pq, item.index)
}

type tribServer struct {
	listener net.Listener
	store    libstore.Libstore
	mux      sync.Mutex
}

const (
	TribbleMax = 100
)

func generateUserKey(user string) string {
	var userToken string
	userToken = ":user"
	return user + userToken
}

func parseUserID(key string) string {
	return strings.Split(key, ":")[0]
}

func generateUserSubscriptionKey(user string) string {
	var subToken string
	subToken = ":subsription"
	return user + subToken
}

func generateUserSubscribersKey(user string) string {
	var subToken string
	subToken = ":subsribers"
	return user + subToken
}

func generateUserTribbleKey(user string) string {
	var tribbleToken string
	tribbleToken = ":tribble"
	return user + tribbleToken
}

func generateTribbleKey(user string, currentTime time.Time) string {
	var tribbleToken string
	tribbleToken = ":subsription"
	return user + tribbleToken + ":" + strconv.FormatInt(currentTime.UnixNano(), 10)
}

func parseTribbleID(key string) string {
	return strings.Split(key, ":")[0]
}

/*
func parseTribble (tribbleInfo string) (string, int) {
	ls := strings.Split(tribbleInfo, ":")
	timeStamp := strconv.Atoi(ls[2])
	return  ls[0], timeStamp
}
*/

func checkInList(elem string, subscribers []string) bool {
	for j := 0; j < len(subscribers); j++ {
		if elem == subscribers[j] {
			return true
		}
	}
	return false
}

// NewTribServer creates, starts and returns a new TribServer. masterServerHostPort
// is the master storage server's host:port and port is this port number on which
// the TribServer should listen. A non-nil error should be returned if the TribServer
// could not be started.
//
// For hints on how to properly setup RPC, see the rpc/tribrpc package.
func NewTribServer(masterServerHostPort, myHostPort string) (TribServer, error) {
	ts := new(tribServer)

	store, err := libstore.NewLibstore(masterServerHostPort, myHostPort, libstore.Normal)
	if err != nil {
		return nil, err
	}

	// Create the server socket that will listen for incoming RPCs.
	listener, err := net.Listen("tcp", myHostPort)
	if err != nil {
		return nil, err
	}

	ts.listener = listener
	ts.store = store

	rpc.RegisterName("TribServer", tribrpc.Wrap(ts))
	if err != nil {
		return nil, err
	}

	rpc.HandleHTTP()
	go http.Serve(ts.listener, nil)

	return ts, nil
}

func (ts *tribServer) CreateUser(args *tribrpc.CreateUserArgs, reply *tribrpc.CreateUserReply) error {
	ts.mux.Lock()
	defer ts.mux.Unlock()

	newUser := generateUserKey(args.UserID)

	_, err := ts.store.Get(newUser)
	if err == nil {
		reply.Status = tribrpc.Exists
		return nil
	}

	addErr := ts.store.Put(newUser, "")
	if addErr == nil {
		reply.Status = tribrpc.OK
	}
	return addErr
}

func (ts *tribServer) AddSubscription(args *tribrpc.SubscriptionArgs, reply *tribrpc.SubscriptionReply) error {
	ts.mux.Lock()
	defer ts.mux.Unlock()

	currUser := generateUserKey(args.UserID)

	_, err := ts.store.Get(currUser)
	if err != nil {
		reply.Status = tribrpc.NoSuchUser
		return nil
	}

	targetUser := generateUserKey(args.TargetUserID)
	_, targetErr := ts.store.Get(targetUser)
	if targetErr != nil {
		reply.Status = tribrpc.NoSuchTargetUser
		return nil
	}

	// add target user to current user's subscription list
	userSubscriptions := generateUserSubscriptionKey(args.UserID)
	appendErr := ts.store.AppendToList(userSubscriptions, targetUser)
	if appendErr != nil {
		reply.Status = tribrpc.Exists
	} else {
		reply.Status = tribrpc.OK
	}

	// add current user to target user's subscribers list
	targetUserSubscribers := generateUserSubscribersKey(args.TargetUserID)
	ts.store.AppendToList(targetUserSubscribers, currUser)

	return nil
}

func (ts *tribServer) RemoveSubscription(args *tribrpc.SubscriptionArgs, reply *tribrpc.SubscriptionReply) error {
	ts.mux.Lock()
	defer ts.mux.Unlock()

	currUser := generateUserKey(args.UserID)
	_, err := ts.store.Get(currUser)
	if err != nil {
		reply.Status = tribrpc.NoSuchUser
		return nil
	}

	targetUser := generateUserKey(args.TargetUserID)
	_, targetErr := ts.store.Get(targetUser)
	if targetErr != nil {
		reply.Status = tribrpc.NoSuchTargetUser
		return nil
	}

	// remove target user from current user's subscription list
	userSubscriptions := generateUserSubscriptionKey(args.UserID)
	removeErr := ts.store.RemoveFromList(userSubscriptions, targetUser)
	if removeErr != nil {
		reply.Status = tribrpc.NoSuchTargetUser
	} else {
		reply.Status = tribrpc.OK
	}

	// remove current user from target user's subscribers list
	targetUserSubscribers := generateUserSubscribersKey(args.UserID)
	ts.store.RemoveFromList(targetUserSubscribers, currUser)

	return nil
}

// func (ts *tribServer) findSubscribers(userID) ([]string, error, bool) {
// 	userInfo := generateUserKey(userID)

// 	_, err := ts.store.Get(userInfo)
// 	if err != nil {
// 		return nil, nil, false
// 	}

// 	userSubKey := generateUserSubscriptionKey(userID)
// 	subscribers, findErr := ts.store.GetList(userSubKey)
// 	if findErr != nil {
// 		return nil, findErr, false
// 	} else {
// 		return subscribers, nil, true
// 	}
// }

func (ts *tribServer) GetFriends(args *tribrpc.GetFriendsArgs, reply *tribrpc.GetFriendsReply) error {
	ts.mux.Lock()
	defer ts.mux.Unlock()

	currUser := generateUserKey(args.UserID)
	_, err := ts.store.Get(currUser)
	if err != nil {
		reply.Status = tribrpc.NoSuchUser
		return nil
	}

	userSubscriptionKey := generateUserSubscriptionKey(args.UserID)
	// get current user's subscription list
	userSubscriptions, getSubscriptionErr := ts.store.GetList(userSubscriptionKey)
	if getSubscriptionErr != nil {
		reply.Status = tribrpc.OK
		return nil
	}

	userSubscribersKey := generateUserSubscribersKey(args.UserID)
	userSubscribers, getSubscribersErr := ts.store.GetList(userSubscribersKey)
	if getSubscribersErr != nil {
		reply.Status = tribrpc.OK
		return nil
	}

	var friends []string
	for _, subUserKey := range userSubscriptions {
		if checkInList(subUserKey, userSubscribers) {
			friends = append(friends, parseUserID(subUserKey))
		}
	}

	reply.UserIDs = friends
	reply.Status = tribrpc.OK
	return nil

}

func (ts *tribServer) PostTribble(args *tribrpc.PostTribbleArgs, reply *tribrpc.PostTribbleReply) error {
	ts.mux.Lock()
	defer ts.mux.Unlock()

	userInfo := generateUserKey(args.UserID)
	_, err := ts.store.Get(userInfo)
	if err != nil {
		reply.Status = tribrpc.NoSuchUser
		return nil
	}

	currentTime := time.Now()
	userTribbleID := generateUserTribbleKey(args.UserID)
	postKey := generateTribbleKey(args.UserID, currentTime)
	newTribble := &tribrpc.Tribble{
		UserID:   args.UserID,
		Posted:   currentTime,
		Contents: args.Contents,
	}
	tribble, marshErr := json.Marshal(newTribble)
	if marshErr != nil {
		return marshErr
	}

	putErr := ts.store.Put(postKey, string(tribble))
	if putErr != nil {
		return putErr
	}

	appendErr := ts.store.AppendToList(userTribbleID, postKey)
	if appendErr != nil {
		return appendErr
	}

	reply.PostKey = postKey
	reply.Status = tribrpc.OK
	return nil
}

func (ts *tribServer) DeleteTribble(args *tribrpc.DeleteTribbleArgs, reply *tribrpc.DeleteTribbleReply) error {
	ts.mux.Lock()
	defer ts.mux.Unlock()

	userInfo := generateUserKey(args.UserID)
	_, err := ts.store.Get(userInfo)
	if err != nil {
		reply.Status = tribrpc.NoSuchUser
		return nil
	}

	userTribbleID := generateUserTribbleKey(args.UserID)
	removeErr := ts.store.RemoveFromList(userTribbleID, args.PostKey)
	if removeErr != nil {
		reply.Status = tribrpc.NoSuchPost
		return nil
	}

	deleteErr := ts.store.Delete(args.PostKey)
	if deleteErr != nil {
		reply.Status = tribrpc.NoSuchPost
		return nil
	}

	reply.Status = tribrpc.OK
	return nil
}

func (ts *tribServer) GetTribbles(args *tribrpc.GetTribblesArgs, reply *tribrpc.GetTribblesReply) error {
	ts.mux.Lock()
	defer ts.mux.Unlock()

	userInfo := generateUserKey(args.UserID)
	_, err := ts.store.Get(userInfo)
	if err != nil {
		reply.Status = tribrpc.NoSuchUser
		return nil
	}

	var tribbles []tribrpc.Tribble
	userTribbleKey := generateUserTribbleKey(args.UserID)
	tribKeyList, listErr := ts.store.GetList(userTribbleKey)
	if listErr != nil {
		reply.Status = tribrpc.OK
		reply.Tribbles = tribbles
		return nil
	}

	for i, _ := range tribKeyList {
		tribKey := tribKeyList[len(tribKeyList)-1-i]
		marshalledTribbleString, getErr := ts.store.Get(tribKey)
		if getErr != nil {
			continue
		}

		var tribble tribrpc.Tribble
		json.Unmarshal([]byte(marshalledTribbleString), &tribble)
		tribbles = append(tribbles, tribble)

		if len(tribbles) >= TribbleMax {
			break
		}
	}
	reply.Tribbles = tribbles
	reply.Status = tribrpc.OK

	return nil
}

func (ts *tribServer) GetTribblesBySubscription(args *tribrpc.GetTribblesArgs, reply *tribrpc.GetTribblesReply) error {
	ts.mux.Lock()
	defer ts.mux.Unlock()

	// find current user
	userInfo := generateUserKey(args.UserID)
	_, err := ts.store.Get(userInfo)
	if err != nil {
		reply.Status = tribrpc.NoSuchUser
		return nil
	}
	// get current user's subscription list
	userSubscriptionKey := generateUserSubscriptionKey(args.UserID)
	subscriptionList, getSubErr := ts.store.GetList(userSubscriptionKey)
	if getSubErr != nil {
		reply.Status = tribrpc.OK
		reply.Tribbles = nil
		return nil
	}

	var pq Queue
	var allTribKeys [][]string
	// iterate through all subscriptions and get their tribbleKeys
	for _, subUserKey := range subscriptionList {
		// get target user
		subUserID := parseUserID(subUserKey)
		subUserTribKey := generateUserTribbleKey(subUserID)
		// get target user tribKey list
		subUserTribKeyList, getTribListErr := ts.store.GetList(subUserTribKey)
		if getTribListErr != nil {
			continue
		}
		allTribKeys = append(allTribKeys, subUserTribKeyList)
	}

	var result []tribrpc.Tribble
	if len(allTribKeys) == 0 {
		reply.Status = tribrpc.OK
		reply.Tribbles = result
		return nil
	}

	// track the latest tribble that is not added to result
	indexTracker := make([]int, len(allTribKeys))
	for i, subscriber := range allTribKeys {
		if len(subscriber) == 0 {
			continue
		}
		indexTracker[i] = len(subscriber) - 1
		lastTribKey := subscriber[len(subscriber)-1]
		tribble, err := ts.getTribble(lastTribKey)
		if err != nil {
			continue
		}
		newElem := &pqElem{
			timeStamp: tribble.Posted,
			source:    i,
			user:      tribble.UserID,
			key:       lastTribKey,
		}
		heap.Push(&pq, newElem)
		indexTracker[i] -= 1
	}

	for pq.Len() > 0 {
		if len(result) >= TribbleMax {
			break
		}
		elem := heap.Pop(&pq).(*pqElem)
		source := elem.source
		tribKey := elem.key
		tribRes, err := ts.getTribble(tribKey)

		if err != nil {
			continue
		}

		result = append(result, tribRes)
		idx := indexTracker[source]
		subscriber := allTribKeys[source]

		if indexTracker[source] >= 0 {
			nextTribKey := subscriber[idx]
			tribble, err := ts.getTribble(nextTribKey)
			if err != nil {
				continue
			}
			newElem := &pqElem{
				timeStamp: tribble.Posted,
				source:    source,
				user:      tribble.UserID,
				key:       nextTribKey,
			}
			heap.Push(&pq, newElem)
			indexTracker[source] -= 1
		}
	}
	reply.Status = tribrpc.OK
	reply.Tribbles = result
	return nil
}

func (ts *tribServer) getTribble(tribKey string) (tribrpc.Tribble, error) {
	content, err := ts.store.Get(tribKey)
	var tribble tribrpc.Tribble
	if err != nil {
		return tribble, err
	}
	json.Unmarshal([]byte(content), &tribble)
	return tribble, nil
}
