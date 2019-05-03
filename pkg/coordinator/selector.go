package coordinator

type RandomServerSelector struct {
	serverAliases map[uint64]bool
}

func MakeRandomServerSelector() *RandomServerSelector {
	return &RandomServerSelector{serverAliases: make(map[uint64]bool)}
}

func (r *RandomServerSelector) ServerRegistered(server *Peer) {
	r.serverAliases[server.Alias] = true
}

func (r *RandomServerSelector) ServerUnregistered(server *Peer) {
	delete(r.serverAliases, server.Alias)
}

func (r *RandomServerSelector) GetServerAliasList(forPeer *Peer) []uint64 {
	peers := make([]uint64, 0, len(r.serverAliases))

	for alias := range r.serverAliases {
		peers = append(peers, alias)
	}

	return peers
}
