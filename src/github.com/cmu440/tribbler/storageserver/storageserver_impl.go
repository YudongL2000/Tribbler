package storageserver

import (
	"errors"
	"sync"

	"github.com/cmu440/tribbler/rpc/storagerpc"
)

type serverRole string

const (
	MasterServer serverRole = "MASTER"
	SlaveServer serverRole = "SLAVE"
)

type storageServer struct {
	isAlive bool       // DO NOT MODIFY
	mux     sync.Mutex // DO NOT MODIFY

	// TODO: implement this!
	role serverRole
	serverNodes []storagerpc.Node
	node storagerpc.Node

	masterAddr string // for slave servers

	port int
	numNodes int
	virtualIDs []uint32

	readyChan chan bool

	serverJoinChan chan storagerpc.Node
	serverJoinReplyChan chan storagerpc.RegisterReply

	itemStorage map[string]string
	listStorage map[string][]string
}

// USED FOR TESTS, DO NOT MODIFY
func (ss *storageServer) SetAlive(alive bool) {
	ss.mux.Lock()
	ss.isAlive = alive
	ss.mux.Unlock()
}

// NewStorageServer creates and starts a new StorageServer. masterServerHostPort
// is the master storage server's host:port address. If empty, then this server
// is the master; otherwise, this server is a slave. numNodes is the total number of
// servers in the ring. port is the port number that this server should listen on.
// virtualIDs is a list of random, unsigned 32-bits IDs identifying this server.
//
// This function should return only once all storage servers have joined the ring,
// and should return a non-nil error if the storage server could not be started.
func NewStorageServer(masterServerHostPort string, numNodes, port int, virtualIDs []uint32) (StorageServer, error) {
	/****************************** DO NOT MODIFY! ******************************/
	ss := new(storageServer)
	ss.isAlive = true
	/****************************************************************************/

	// TODO: implement this!
	if masterServerHostPort == nil || masterServerHostPort == "" {
		ss.role = MasterServer
		masterAddr = nil
	} else {
		ss.role = SlaveServer
		masterAddr = masterServerHostPort
	}

	ss.node := Node { HostPort: 'localhost:' + strconv(port), virtualIDs: virtualIDs }
	ss.itemStorage = make(map[string]string)
	ss.listStorage = make(map[string][]string)
	
	if ss.role == MasterServer {
		ss.mux.Lock()
		ss.serverNodes = append(ss.serverNodes, node)
		ss.mux.Unlock()
	} else {
		// ignore for checkpoint
	}

	listener, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
    if err != nil {
        return nil, err
    }

	err = rpc.RegisterName("StorageServer", storagerpc.Wrap(ss))
    if err != nil {
        return nil, err
	}
	
	rpc.HandleHTTP()
    go http.Serve(listener, nil)

	if ss.role == MasterServer {
		ss.mux.Lock()
		ss.serverNodes = append(ss.serverNodes, storagerpc.Node { 
			HostPort: 'localhost:' + strconv(port), 
			virtualIDs: virtualIDs, 
		})
		ss.mux.Unlock()

		for {
			newNode := <- ss.serverJoinChan

			ss.mux.Lock()
			exists := false
			for _, n in range(ss.serverNodes) {
				if n == newNode {
					exists = true
				}
			}
			if !exists {
				ss.serverNodes = append(ss.serverNodes, newNode)
			}

			if len(ss.serverNodes) == ss.numNodes {
				ss.serverJoinReplyChan <- storagerpc.RegisterReply {
					Status: storagerpc.OK,
					Servers: ss.serverNodes,
				}
				ss.mux.Unlock()
				break
			}
			 
			ss.serverJoinReplyChan <- storagerpc.RegisterReply {
				Status: storagerpc.NotReady,
			}
			ss.mux.Unlock()
		}
	} else {
		// ignored for checkpoint
	}

	err := nil
	return ss, err
}

func (ss *storageServer) registerServer(args *storagerpc.RegisterArgs, reply *storagerpc.RegisterReply) error {
	ss.mux.Lock()
	defer ss.mux.Unlock()
	if ss.role == SlaveServer {
		return errors.New("request sent to slave server")
	} 
	
	if len(ss.serverNodes) < ss.numNodes {
		server := args.ServerInfo
		ss.serverJoinChan <- server
		reply = <- ss.serverJoinReplyChan
	} else {
		reply.Status = storagerpc.OK
		reply.Servers = ss.serverNodes
	}
}

func (ss *storageServer) getServers(args *storagerpc.GetServersArgs, reply *storagerpc.GetServersReply) error {
	ss.mux.Lock()
	defer ss.mux.Unlock()
	if len(ss.serverNodes) < ss.numNodes {
		reply.Status = storagerpc.NotReady
	} else {
		reply.Servers = ss.serverNodes
	}
}

func findStorageServer(ss *storageServer, key string) storagerpc.Node {
	hash := libstore.StoreHash(string.split(key, ':')[0])
	var server storagerpc.Node
	var nearestID uint32 = ^uint32(0)

	for _, n := range(ss.serverNodes) {
		for _, id := range(n.virtualIDs) {
			if id >= hash && id < nearestID {
				nearestID = id
				server = n
			}
		}
	}

	return server
}

func (ss *storageServer) get(args *storagerpc.GetArgs, reply *storagerpc.GetReply) error {
	ss.mux.Lock()
	defer ss.mux.Unlock()

	if ss.node != findStorageServer(ss, args.Key) {
		return errors.New("key doesn't belong to this server")
	}

	value, found := ss.itemStorage[args.Key]
	if !found {
		reply.Status = storagerpc.KeyNotFound
	} else {
		reply.Status = storagerpc.OK
		reply.Value = value
	}

	return nil
}

func (ss *storageServer) delete(args *storagerpc.DeleteArgs, reply *storagerpc.DeleteReply) error {
	ss.mux.Lock()
	defer ss.mux.Unlock()

	if ss.node != findStorageServer(ss, args.Key) {
		return errors.New("key doesn't belong to this server")
	}

	value, found := ss.itemStorage[args.Key]
	if !found {
		reply.Status = storagerpc.KeyNotFound
	} else {
		reply.Status = storagerpc.OK
		delete(ss.storage, args.Key)
	}

	return nil
}

func (ss *storageServer) getList(args *storagerpc.GetArgs, reply *storagerpc.GetListReply) error {
	ss.mux.Lock()
	defer ss.mux.Unlock()

	if ss.node != findStorageServer(ss, args.Key) {
		return errors.New("key doesn't belong to this server")
	}

	value, found := ss.listStorage[args.Key]
	if !found {
		reply.Status = storagerpc.KeyNotFound
	} else {
		reply.Status = storagerpc.OK
		reply.Value = value
	}

	return nil
}

func (ss *storageServer) put(args *storagerpc.PutArgs, reply *storagerpc.PutReply) error {
	ss.mux.Lock()
	defer ss.mux.Unlock()

	if ss.node != findStorageServer(ss, args.Key) {
		return errors.New("key doesn't belong to this server")
	}

	ss.itemStorage[args.Key] = args.Value
	reply.Status = storagerpc.OK

	return nil
}

func (ss *storageServer) appendToList(args *storagerpc.PutArgs, reply *storagerpc.PutReply) error {
	ss.mux.Lock()
	defer ss.mux.Unlock()

	if ss.node != findStorageServer(ss, args.Key) {
		return errors.New("key doesn't belong to this server")
	}

	value, found := ss.listStorage[args.Key]
	reply.Status = storagerpc.OK

	for _, v := range(value) {
		if v == args.Value {
			reply.Status = storagerpc.ItemExists
			break
		}
	}
	ss.listStorage[args.Key] = append(value, args.Value)

	return nil
}

func (ss *storageServer) removeFromList(args *storagerpc.PutArgs, reply *storagerpc.PutReply) error {
	ss.mux.Lock()
	defer ss.mux.Unlock()

	if ss.node != findStorageServer(ss, args.Key) {
		return errors.New("key doesn't belong to this server")
	}

	value, found := ss.listStorage[args.Key]
	if !found {
		reply.Status = storagerpc.KeyNotFound
	} else {
		newList := nil
		for _, v := range(value) {
			if v != args.Value { 
				newList = append(newList, v)
			}
		}
		if len(newList) == len(value) {
			reply.Status = storagerpc.ItemNotFound
		} else {
			reply.Status = storagerpc.OK
		}
	}

	return nil
}
