package coordinator

type RandomServerSelector struct {
	serverAliases map[string]bool
}

func MakeRandomServerSelector() *RandomServerSelector {
	return &RandomServerSelector{serverAliases: make(map[string]bool)}
}

func (r *RandomServerSelector) ServerRegistered(server *Peer) {
	r.serverAliases[server.Alias] = true
}

func (r *RandomServerSelector) ServerUnregistered(server *Peer) {
	delete(r.serverAliases, server.Alias)
}

func (r *RandomServerSelector) GetServerAliasList(forPeer *Peer) []string {
	peers := []string{}

	for alias := range r.serverAliases {
		peers = append(peers, alias)
	}

	return peers
}
