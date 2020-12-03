package libstore

import (
	"errors"
	"github.com/cmu440/tribbler/rpc/librpc"
	"github.com/cmu440/tribbler/rpc/storagerpc"
	"net/rpc"
	"strconv"
	"sync"
	"time"
)

type cacheStringInfo struct {
	value         string
	insertionTime time.Time
	validSeconds  int
}

type cacheListInfo struct {
	value         []string
	insertionTime time.Time
	validSeconds  int
}

type libstore struct {
	// TODO: implement this!
	leaseMode   LeaseMode
	hostport    string
	master      *rpc.Client
	ssClient    []*rpc.Client
	ssServers   []storagerpc.Node
	stringMutex sync.Mutex
	cacheString map[string]*cacheStringInfo
	listMutex   sync.Mutex
	cacheList   map[string]*cacheListInfo
	queryMutex  sync.Mutex
	queryInfo   map[string]int
}

func (ls *libstore) findNode(key string) *rpc.Client {
	return ls.master
}

func (ls *libstore) wantLease(key string) bool {
	if ls.leaseMode == Never {
		return false
	} else if ls.leaseMode == Always {
		return true
	}
	if ls.leaseMode == Normal {
		ls.queryMutex.Lock()
		leaseNum, existence := ls.queryInfo[key]
		if existence {
			ls.queryMutex.Unlock()
			return leaseNum >= storagerpc.QueryCacheThresh
		}
		ls.queryMutex.Unlock()
		return false
	}
	return false
}

func (ls *libstore) addQuery(key string) {
	ls.queryMutex.Lock()
	queryNum, existence := ls.queryInfo[key]
	if existence {
		ls.queryInfo[key] = queryNum + 1
	} else {
		ls.queryInfo[key] = 1
	}
	ls.queryMutex.Unlock()
}

func (ls *libstore) deleteQuery(key string) {
	time.Sleep(storagerpc.QueryCacheSeconds * time.Second)
	ls.queryMutex.Lock()
	queryNum, existence := ls.queryInfo[key]
	if existence {
		ls.queryInfo[key] = queryNum - 1
	}
	ls.queryMutex.Unlock()
}

//delete key from string cache after expiration limit
func (ls *libstore) finishStringLease(key string, timeLimit int) {
	time.Sleep(time.Duration(timeLimit) * time.Second)
	ls.stringMutex.Lock()
	item, existence := ls.cacheString[key]
	if existence && int(time.Since(item.insertionTime).Seconds()) >= item.validSeconds {
		delete(ls.cacheString, key)
	}
	ls.stringMutex.Unlock()
}

//delete key from List cache after expiration limit
func (ls *libstore) finishListLease(key string, timeLimit int) {
	time.Sleep(time.Duration(timeLimit) * time.Second)
	ls.listMutex.Lock()
	item, existence := ls.cacheList[key]
	if existence && int(time.Since(item.insertionTime).Seconds()) >= item.validSeconds {
		delete(ls.cacheList, key)
	}
	ls.listMutex.Unlock()
}

// NewLibstore creates a new instance of a TribServer's libstore. masterServerHostPort
// is the master storage server's host:port. myHostPort is this Libstore's host:port
// (i.e. the callback address that the storage servers should use to send back
// notifications when leases are revoked).
//
// The mode argument is a debugging flag that determines how the Libstore should
// request/handle leases. If mode is Never, then the Libstore should never request
// leases from the storage server (i.e. the GetArgs.WantLease field should always
// be set to false). If mode is Always, then the Libstore should always request
// leases from the storage server (i.e. the GetArgs.WantLease field should always
// be set to true). If mode is Normal, then the Libstore should make its own
// decisions on whether or not a lease should be requested from the storage server,
// based on the requirements specified in the project PDF handout.  Note that the
// value of the mode flag may also determine whether or not the Libstore should
// register to receive RPCs from the storage servers.
//
// To register the Libstore to receive RPCs from the storage servers, the following
// line of code should suffice:
//
//     rpc.RegisterName("LeaseCallbacks", librpc.Wrap(libstore))
//
// Note that unlike in the NewTribServer and NewStorageServer functions, there is no
// need to create a brand new HTTP handler to serve the requests (the Libstore may
// simply reuse the TribServer's HTTP handler since the two run in the same process).
func NewLibstore(masterServerHostPort, myHostPort string, mode LeaseMode) (Libstore, error) {
	ls := &libstore{
		hostport:    myHostPort,
		leaseMode:   mode,
		cacheString: make(map[string]*cacheStringInfo),
		cacheList:   make(map[string]*cacheListInfo),
	}

	err1 := rpc.RegisterName("LeaseCallbacks", librpc.Wrap(ls))
	if err1 != nil {
		return nil, err1
	}

	master, err2 := rpc.DialHTTP("tcp", masterServerHostPort)
	if err2 != nil {
		return nil, err2
	}

	ls.master = master
	args := &storagerpc.GetServersArgs{}
	reply := &storagerpc.GetServersReply{}
	for retry := 0; retry <= 5; retry++ {
		errCall := ls.master.Call("StorageServer.GetServers", args, reply)
		if errCall != nil {
			return nil, errCall
		} else if reply.Status == storagerpc.OK {
			ls.ssServers = reply.Servers
			/*
				for _, server := range(ls.ssServer) {
					var newClient *rpc.Client
					newClient, errDial := rpc.DialHTTP("tcp", server.HostPort)
					if errDial != nil {
						return nil, errDial
					}
					ls.ssClient = append(ls.ssClient, newClient)
				}*/
			break
		}
		time.Sleep(time.Second)
	}

	if reply.Status != storagerpc.OK {
		return nil, errors.New("MasterServer Invalid")
	}
	return ls, nil
}

func (ls *libstore) Get(key string) (string, error) {
	//find the target node for retrieving the information
	ls.stringMutex.Lock()
	targetClient := ls.findNode(key)
	wantLease := ls.wantLease(key)
	currentTime := time.Now()
	item, existence := ls.cacheString[key]
	if existence && wantLease {
		//already in the cache and currently in time limit
		if int(time.Since(item.insertionTime).Seconds()) < item.validSeconds {
			ls.stringMutex.Unlock()
			return item.value, nil
		} else {
			// in cache but information expired
			delete(ls.cacheString, key)
		}
	}
	ls.stringMutex.Unlock()
	args := &storagerpc.GetArgs{
		Key:       key,
		WantLease: wantLease,
		HostPort:  ls.hostport,
	}
	var reply storagerpc.GetReply
	//Retrieve information from storageServer
	err := targetClient.Call("StorageServer.Get", args, &reply)
	if err != nil {
		return "", err
	}

	if reply.Status != storagerpc.OK {
		return reply.Value, errors.New("Get request Error. Status: " + strconv.Itoa(int(reply.Status)))
	}

	// place the new information inside the cache
	if wantLease && reply.Lease.Granted {
		ls.stringMutex.Lock()
		ls.cacheString[key] = &cacheStringInfo{
			value:         reply.Value,
			insertionTime: currentTime,
			validSeconds:  reply.Lease.ValidSeconds,
		}
		ls.stringMutex.Unlock()
		//wait till it expire
		go ls.finishStringLease(key, reply.Lease.ValidSeconds)
		ls.addQuery(key)
		go ls.deleteQuery(key)
	}
	return reply.Value, nil
}

func (ls *libstore) Put(key, value string) error {
	targetClient := ls.findNode(key)
	args := &storagerpc.PutArgs{
		Key:   key,
		Value: value,
	}
	var reply storagerpc.PutReply
	errCall := targetClient.Call("StorageServer.Put", args, &reply)
	if errCall != nil {
		return errCall
	}
	if reply.Status != storagerpc.OK {
		return errors.New("Put request Error. Status: " + strconv.Itoa(int(reply.Status)))
	}
	return nil
}

func (ls *libstore) Delete(key string) error {
	targetClient := ls.findNode(key)
	args := &storagerpc.DeleteArgs{
		Key: key,
	}
	var reply storagerpc.DeleteReply
	errCall := targetClient.Call("StorageServer.Delete", args, &reply)
	if errCall != nil {
		return errCall
	}
	if reply.Status != storagerpc.OK {
		return errors.New("Delete request Error. Status: " + strconv.Itoa(int(reply.Status)))
	}
	return nil
}

func (ls *libstore) GetList(key string) ([]string, error) {
	ls.listMutex.Lock()
	targetClient := ls.findNode(key)
	wantLease := ls.wantLease(key)
	currentTime := time.Now()
	item, existence := ls.cacheList[key]
	if existence && wantLease {
		//already in the cache and currently in time limit
		if int(time.Since(item.insertionTime).Seconds()) < item.validSeconds {
			ls.listMutex.Unlock()
			return item.value, nil
		} else {
			// in cache but information expired
			delete(ls.cacheList, key)
		}
	}
	ls.listMutex.Unlock()
	args := &storagerpc.GetArgs{
		Key:       key,
		WantLease: wantLease,
		HostPort:  ls.hostport,
	}
	var reply storagerpc.GetListReply
	//Retrieve information from storageServer
	err := targetClient.Call("StorageServer.GetList", args, &reply)
	if err != nil {
		return nil, err
	}

	if reply.Status != storagerpc.OK {
		return reply.Value, errors.New("GetList request Error. Status: " + strconv.Itoa(int(reply.Status)))
	}

	// place the new information inside the cache
	if wantLease && reply.Lease.Granted {
		ls.listMutex.Lock()
		ls.cacheList[key] = &cacheListInfo{
			value:         reply.Value,
			insertionTime: currentTime,
			validSeconds:  reply.Lease.ValidSeconds,
		}
		ls.listMutex.Unlock()
		//wait till it expire
		go ls.finishListLease(key, reply.Lease.ValidSeconds)
		ls.addQuery(key)
		go ls.deleteQuery(key)
	}
	return reply.Value, nil
}

func (ls *libstore) RemoveFromList(key, removeItem string) error {
	targetClient := ls.findNode(key)
	args := &storagerpc.PutArgs{
		Key:   key,
		Value: removeItem,
	}
	var reply storagerpc.PutReply
	errCall := targetClient.Call("StorageServer.RemoveFromList", args, &reply)
	if errCall != nil {
		return errCall
	}

	if reply.Status != storagerpc.OK {
		return errors.New("RemoveFromList request Error. Status: " + strconv.Itoa(int(reply.Status)))
	}
	return nil
}

func (ls *libstore) AppendToList(key, newItem string) error {
	targetClient := ls.findNode(key)
	args := &storagerpc.PutArgs{
		Key:   key,
		Value: newItem,
	}
	var reply storagerpc.PutReply
	errCall := targetClient.Call("StorageServer.AppendToList", args, &reply)
	if errCall != nil {
		return errCall
	}

	if reply.Status != storagerpc.OK {
		return errors.New("AppendToList request Error. Status: " + strconv.Itoa(int(reply.Status)))
	}
	return nil
}

func (ls *libstore) RevokeLease(args *storagerpc.RevokeLeaseArgs, reply *storagerpc.RevokeLeaseReply) error {
	_, inStringCache := ls.cacheString[args.Key]
	if inStringCache {
		delete(ls.cacheString, args.Key)
		reply.Status = storagerpc.OK
		return nil
	}

	_, inListCache := ls.cacheList[args.Key]
	if inListCache {
		delete(ls.cacheList, args.Key)
		reply.Status = storagerpc.OK
	} else {
		reply.Status = storagerpc.KeyNotFound
	}
	return nil
}
