# WebRTC message broker

## Components

![](docs/diagram.png?raw=true)

### Clients

The whole point of this system is to provide connectivity to the clients. The clients connect to a single communication server after credentials negotiation with the Coordinator Server.

### Coordinator Server

The coordinator server is the key entry point of our communications system.
- It exposes a WS endpoint for the clients to negotiate the communication with the communications server
    - Caveat: It’s very important to notice the coordinator server will choose a communication server randomly, which means all communication servers should be equivalent to each other. That is, a client connected to a cluster, should have a consistent latency no matter which communication server he ends connecting to.
- It exposes a WS endpoint for the communication servers to
    - Negotiate connections with the clients
    - Discover other communications server in the cluster


### Communication server

The communications server is the heavy lifter of the system.

- It connects to the Coordinator Server via WS and uses this connection to discover other servers and negotiate client connections
- Every communication server may be connected to clients and
    - It relays packets from the clients to other clients
    - It relays packets to all the connected servers
    - It handles the business logic of the packets (topics, etc)
- It has to keep the WS connection alive, always. If the connection is closed, it has to retry until success.
