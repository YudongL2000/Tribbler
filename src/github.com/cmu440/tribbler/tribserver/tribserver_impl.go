package tribserver

import (
	"container/heap"
	"errors"
	"github.com/cmu440/tribbler/rpc/libstore"
	"math"
	"log"
	"strings"
	"time"
	"strconv"
	"encoding/json"
	"net/rpc"
	"github.com/cmu440/tribbler/rpc/tribrpc"
)


//*****************************Implementation of a Priority Queue*******************
type pqElem struct {
	index  int 
	timeStamp  int
	source int
	user  string
}

type Queue []*pqElem

func (pq Queue) Len() int {
	return len(pq)
}

func (pq Queue) Less(i, j int) bool {
	return pq[i].timeStamp > pq[j].timeStamp
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

func (pq *Queue) update(item *pqElem, userID string, timeStamp int) {
	item.user = userID
	item.timeStamp = timeStamp
	heap.Fix(pq, item.index)
}

type tribServer struct {
	listener net.Listener
	store libstore.Libstore
}



const (
	TribbleMax = 100
)

func generateUserID (user string) string {
	var userToken string
	userToken = ":user"
	return user+userToken
}

func generateUserSubKey (user string) string {
	var subToken string
	subToken = ":subsription"
	return user+subToken
}

func generateTribbleID (user string, currentTime time.Time) string {
	var tribbleToken string
	tribbleToken = ":subsription"
	return user + tribbleToken + ":" + strconv.FormatInt(currentTime.UnixNano(), 10)
}

func generateUserTribbleID (user string) string {
	var tribbleToken string
	tribbleToken = ":tribble"
	return user+tribbleToken
}

func parseTribble (tribbleInfo string) (string, int) {
	ls := strings.Split(tribbleInfo, ":")
	timeStamp := strconv.Atoi(ls[2])
	return  ls[0], timeStamp
}


func checkInList (elem string, subscribers []string) bool {
	for j:=0; j<len(subscribers); j++ {
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
	store, err := libstore.NewLibstore(masterServerHostPort, myHostPort, libstore.Normal)
	if err != nil {
		return nil, err
	}

	// Create the server socket that will listen for incoming RPCs.
	listener, err := net.Listen("tcp", myHostPort)
	if err != nil {
		return nil, err
	}
	ts := &tribServer {
		listener: listener,
		store: store
	}
	if err = rpc.RegisterName("TribServer", tribrpc.Wrap(ts)) {
		if err != nil {
			return nil, err
		}
	}
	rpc.HandleHTTP()
	go http.Serve(ts.listener, nil)
	return ts, nil
}

func (ts *tribServer) CreateUser(args *tribrpc.CreateUserArgs, reply *tribrpc.CreateUserReply) error {
	newUser := generateUserID(args.UserID)
	_, err := ts.store.Get(newUser)
	if err != nil {
		addErr := ts.store.Put(newUser, "")
		if (addErr == nil) {
			reply.Status = tribrpc.OK
			return nil
		} else {
			return addErr
		}
	} else {
		reply.Status = tribrpc.OK
		return nil
	}
}

func (ts *tribServer) AddSubscription(args *tribrpc.SubscriptionArgs, reply *tribrpc.SubscriptionReply) error {
	userInfo := generateUserID(args.UserID)
	_, err := ts.store.Get(userInfo)
	if (err != nil) {
		reply.Status = tribrpc.NoSuchUser
		return nil
	} else {
		targetUser := generateUserID(args.TargetUserID)
		_, targetErr := ts.store.Get(targetUser)
		if targetErr != nil {
			reply.Status = tribrpc.NoSuchTargetUser
			return nil
		} else {
			userSubKey := generateUserSubKey(args.UserID)
			appendErr := ts.store.AppendToList(userSubKey, targetUser)
			if appendErr != nil {
				reply.Status = tribrpc.Exists
			} else {
				reply.Status = tribrpc.OK
			}
			return nil
		}
	}
}

func (ts *tribServer) RemoveSubscription(args *tribrpc.SubscriptionArgs, reply *tribrpc.SubscriptionReply) error {
	userInfo := generateUserID(args.UserID)
	_, err := ts.store.Get(userInfo)
	if (err != nil) {
		reply.Status = tribrpc.NoSuchUser
		return nil
	} else {
		targetUser := generateUserID(args.TargetUserID)
		_, targetErr := ts.store.Get(targetUser)
		if targetErr != nil {
			reply.Status = tribrpc.NoSuchTargetUser
			return nil
		} else {
			userSubKey := generateUserSubKey(args.UserID)
			removeErr := ts.store.RemoveFromList(userSubKey, targetUser)
			if removeErr != nil {
				reply.Status = tribrpc.NoSuchTargetUser 
			} else {
				reply.Status = tribrpc.OK
			}
			return nil
		}
	}
}

func (ts *tribServer) findSubscribers(userID) ([]string, error, bool) {
	userInfo := generateUserID(userID)
	_, err := ts.store.Get(userInfo)
	if err != nil {
		return nil, nil, false
	} else {
		userSubKey := generateUserSubKey(userID)
		subscribers, findErr := ts.store.GetList(userSubKey)
		if findErr != nil {
			return nil, findErr, false
		} else {
			return subscribers, nil, true
		}
	}
}

func (ts *tribServer) GetFriends(args *tribrpc.GetFriendsArgs, reply *tribrpc.GetFriendsReply) error {
	subscribers, err, valid:= ts.findSubscribers(args.UserID)
	res := []string{}
	if err != nil {
		return err
	} else if valid == false {
		reply.Status = tribrpc.NoSuchUser
		return nil
	} else {
		userInfo := generateUserID(args.UserID)
		for i:= 0; i<len(subscribers); i++ {
			subID := subscribers[i]
			subsubscribers, subErr, valid := ts.findSubscribers(subID)
			if subErr != nil || valid == false{
				continue
			} else {
				isFriend := checkInList(args.UserID, subsubscribers)
				if isFriend {
					res = append(res, subID)
				}
			}
		}
	}
	return res
}

func (ts *tribServer) PostTribble(args *tribrpc.PostTribbleArgs, reply *tribrpc.PostTribbleReply) error {
	userInfo := generateUserID(userID)
	_, err := ts.store.Get(userInfo)
	if (err != nil) {
		reply.Status = tribrpc.NoSuchUser
		return nil
	} else {
		currentTime := time.Now()
		userTribbleID := generateUserTribbleID(args.UserID)
		tribbleTimeID := generateTribbleID(args.UserID,currentTime)
		newTribble := &tribrpc.Tribble {
			UserID: args.UserID,
			Posted: currentTime,
			Contents: args.Contents,
		}
		marshalledTribble, marshErr := json.Marshal(newTribble)
		if marshErr == nil {
			putErr := ts.store.Put(tribbleTimeID, string(marshalledTribble))
			if (putErr != nil) {
				return putErr
			} 
			appendErr := ts.store.AppendToList(userTribbleID,tribbleTimeID)
			if appendErr == nil {
				reply.PostKey = tribbleTimeID
				reply.Status = tribrpc.OK
				return nil
			} else {
				return appendErr
			}
		} else {
			return marshErr
		}
	}
}

func (ts *tribServer) DeleteTribble(args *tribrpc.DeleteTribbleArgs, reply *tribrpc.DeleteTribbleReply) error {
	userInfo := generateUserID(userID)
	_, err := ts.store.Get(userInfo)
	if (err != nil) {
		reply.Status = tribrpc.NoSuchUser
		return nil
	} else {
		userTribbleID := generateUserTribbleID(args.UserID)
		removeErr := ts.store.RemoveFromList(userTribbleID, args.PostKey)
		if (removeErr != nil) {
			return removeErr
		}
		deleteErr := ts.store.Delete(args.PostKey)
		reply.Status = tribrpc.OK
		return nil
	}
}

func (ts *tribServer) GetTribbles(args *tribrpc.GetTribblesArgs, reply *tribrpc.GetTribblesReply) error {
	userInfo := generateUserID(userID)
	_, err := ts.store.Get(userInfo)
	if (err != nil) {
		reply.Status = tribrpc.NoSuchUser
		return nil
	} else {
		userTribbleID := generateUserTribbleID(args.UserID)
		tribSubscribers, listErr := ts.store.GetList(userTribbleID)
		if listErr != nil {
			return listErr
		} else {
			var tribbles [TribbleMax]tribrpc.Tribble
			tribbleCount := 0
			for i := len(tribSubscribers) - 1; i >= 0 ; i-- {
				tmp := tribSubscribers[i]
				var tribble tribrpc.Tribble
				marshalledTribble, getErr := ts.store.Get(tmp)
				if getErr != nil {
					continue
				} else {
					json.Unmarshal(([]byte)marshalledTribble), &tribble)
					tribbles[tribbleCount] = tribble
					tribbleCount++
					if (tribbleCount >= TribbleMax) {
						break
					}
				}
			}
			reply.Tribbles = tribbles
			reply.Status = tribrpc.OK
			return nil
		}
	}
}

func (ts *tribServer) GetTribblesBySubscription(args *tribrpc.GetTribblesArgs, reply *tribrpc.GetTribblesReply) error {
	userInfo := generateUserID(args.UserID)
	_, err = ts.storage.Get(userInfo)
	if (err != nil) {
		reply.Status = tribrpc.NoSuchUser
		return nil
	} else {
		userTribbleID := generateUserTribbleID(args.UserID)
		var subList []string
		if subList, getErr = ts.storage.GetList(userTribbleID) {
			if (getErr != nil) {
				return getErr
			} else if len(subList) == 0 {
				reply.Status = tribrpc.OK
				reply.Tribbles = nil
				return nil
			} else {
				var pq Queue
				var tribList [][]string
				for i:= 0; i<len(subList); i++ {
					subUserID := subList[i]
					subUserIDTrib := generateUserTribbleID(subUserID)
					subUserList, listErr := ts.store.GetList(subUserIDTrib)
					if (listErr != nil) {
						return listErr
					} else {
						tribList = append(tribList, subUserList)
					}
				}
				tribCount := 0
				for j:= 0; j<len(subList); j++ {
					if (len(tribList[j]) == 0) {
						continue
					} else {
						listLen := len(tribList[j])
						latestTrib := tribList[j][listLen-1]
						_,timeStamp :=  parseTribble(latestTrib)
						newElem := &pqElem {
							timeStamp : timeStamp,
							user: latestTrib,
							source: j,
						}
						heap.Push(&pq, newElem)
						tribList[j] = tribList[j][:listLen-1]
					}
				}
				res := []tribrpc.Tribble{}
				for (len(res) < TribbleMax && pq.Len()>0) {
					elem := heap.Pop(&pq).(*pqElem)
					var newTribble tribrpc.Tribble
					marshalledTribble, getErr := ts.store.Get(elem.user)
					if (getErr == nil) {
						json.Unmarshal(([]byte)marshalledTribble, &newTribble)
						res = append(res, newTribble)
					}
					idx := elem.source
					if idx<len(tribList) && (tribList[idx]) > 0 {
						currentLen := len(tribList[idx])
						nextTrib := tribList[idx][currentLen-1]
						_,timeStamp :=  parseTribble(nextTrib)
						newElem := &pqElem {
							timeStamp : timeStamp,
							user: nextTrib,
							source: idx,
						}
						heap.Push(&pq, newElem)
						tribList[idx] = tribList[idx][:currentLen-1]
					}
				}
				reply.Status = tribrpc.OK
				reply.Tribbles = res
				return nil
			}
		}
	}
}
