package coordinator

import (
	"math/rand"
)

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

func (r *RandomServerSelector) Select(state *CoordinatorState, forPeer *Peer) *Peer {
	// TODO: just a hack to get a random server, let's build something better
	s := len(r.serverAliases)
	if s == 0 {
		return nil
	}
	t := rand.Intn(s)
	i := 0

	for serverAlias := range r.serverAliases {
		if i == t {
			server := state.Peers[serverAlias]
			if !server.isClosed {
				return server
			}
		}
		i += 1
	}
	return nil
}

func (r *RandomServerSelector) GetServerAliasList(forPeer *Peer) []string {
	peers := []string{}

	for alias := range r.serverAliases {
		peers = append(peers, alias)
	}

	return peers
}
