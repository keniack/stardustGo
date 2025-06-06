package links

import (
	"errors"
	"sync"

	"github.com/keniack/stardustGo/configs"
	linkmod "github.com/keniack/stardustGo/internal/links/linktypes"
	satmod "github.com/keniack/stardustGo/internal/node"
	"github.com/keniack/stardustGo/pkg/types"
)

// IslSatelliteCentricMstProtocol implements a satellite-centric MST algorithm
// for managing inter-satellite links. It ensures links are established optimally
// based on distance, using a priority queue to build the MST.
type IslSatelliteCentricMstProtocol struct {
	links       []*linkmod.IslLink // All candidate links
	established []*linkmod.IslLink // Currently established MST links

	satellite  *satmod.Satellite   // The local satellite this protocol is mounted to
	satellites []*satmod.Satellite // Cache of reachable satellites
	position   types.Vector        // Last position at which MST was calculated

	visited map[*satmod.Satellite]bool // Visited set for MST
	pq      linkmod.LinkPriorityQueue  // Priority queue for link selection
	mu      sync.Mutex                 // Protects state access

	readyCh chan struct{} // Signals when MST update is finished
}

// NewIslSatelliteCentricMstProtocol creates a new MST-based link manager
func NewIslSatelliteCentricMstProtocol() *IslSatelliteCentricMstProtocol {
	return &IslSatelliteCentricMstProtocol{
		links:       []*linkmod.IslLink{},
		established: []*linkmod.IslLink{},
		visited:     make(map[*satmod.Satellite]bool),
		pq:          *linkmod.NewLinkPriorityQueue(),
		readyCh:     make(chan struct{}, 1),
	}
}

// Mount binds the protocol to a satellite
func (p *IslSatelliteCentricMstProtocol) Mount(sat types.INode) {
	if p.satellite == nil {
		if s, ok := sat.(*satmod.Satellite); ok {
			p.satellite = s
		}
	}
}

// AddLink adds a possible link between satellites to be considered in MST
func (p *IslSatelliteCentricMstProtocol) AddLink(link types.ILink) {
	if isl, ok := link.(*linkmod.IslLink); ok {
		p.mu.Lock()
		defer p.mu.Unlock()
		p.links = append(p.links, isl)
	}
}

// ConnectSatellite is not implemented in this strategy
func (p *IslSatelliteCentricMstProtocol) ConnectSatellite(s types.INode) error {
	return errors.New("ConnectSatellite not implemented")
}

// ConnectLink immediately establishes the given link
func (p *IslSatelliteCentricMstProtocol) ConnectLink(link types.ILink) error {
	if isl, ok := link.(*linkmod.IslLink); ok {
		p.mu.Lock()
		defer p.mu.Unlock()
		p.established = append(p.established, isl)
	}
	return nil
}

// DisconnectSatellite is not implemented in this strategy
func (p *IslSatelliteCentricMstProtocol) DisconnectSatellite(s types.INode) error {
	return errors.New("DisconnectSatellite not implemented")
}

// DisconnectLink removes a link from the list of established connections
func (p *IslSatelliteCentricMstProtocol) DisconnectLink(link types.ILink) error {
	if isl, ok := link.(*linkmod.IslLink); ok {
		p.mu.Lock()
		defer p.mu.Unlock()
		for i, l := range p.established {
			if l == isl {
				p.established = append(p.established[:i], p.established[i+1:]...)
				break
			}
		}
	}
	return nil
}

// UpdateLinks calculates a minimum spanning tree of inter-satellite links
// If position hasn't changed, returns cached result
func (p *IslSatelliteCentricMstProtocol) UpdateLinks() ([]types.ILink, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.satellite == nil {
		return nil, errors.New("satellite not mounted")
	}

	if p.position.Equals(p.satellite.PositionVector()) {
		select {
		case <-p.readyCh:
		default:
		}
		result := make([]types.ILink, len(p.established))
		for i, l := range p.established {
			result[i] = l
		}
		return result, nil
	}
	p.position = p.satellite.PositionVector()

	// Build unique set of reachable satellites
	satMap := map[*satmod.Satellite]bool{}
	for _, link := range p.links {
		if n1, ok := link.Node1.(*satmod.Satellite); ok {
			satMap[n1] = true
		}
		if n2, ok := link.Node2.(*satmod.Satellite); ok {
			satMap[n2] = true
		}
	}

	p.satellites = make([]*satmod.Satellite, 0, len(satMap))
	for sat := range satMap {
		p.satellites = append(p.satellites, sat)
	}

	p.visited = make(map[*satmod.Satellite]bool)
	p.pq.Clear()

	// Add initial links from the local satellite to the priority queue
	for _, l := range p.links {
		if l.Distance() <= configs.MaxISLDistance {
			p.pq.Enqueue(l, l.Distance())
		}
	}

	mst := []*linkmod.IslLink{}
	p.visited[p.satellite] = true

	// Prim's algorithm loop: pick smallest links without forming cycles
	for len(mst) < len(p.satellites)-1 && p.pq.Len() > 0 {
		link := p.pq.Dequeue()
		if link == nil {
			break
		}

		s1, _ := link.Node1.(*satmod.Satellite)
		s2, _ := link.Node2.(*satmod.Satellite)
		if p.visited[s1] && p.visited[s2] {
			continue
		}

		var newSat *satmod.Satellite
		if !p.visited[s1] {
			newSat = s1
		} else {
			newSat = s2
		}

		// Enqueue all links from newSat to unvisited nodes
		for _, l := range newSat.ISLProtocol.Links() {
			if isl, ok := l.(*linkmod.IslLink); ok && isl.Distance() <= configs.MaxISLDistance {
				s1, _ := isl.Node1.(*satmod.Satellite)
				s2, _ := isl.Node2.(*satmod.Satellite)
				if !(p.visited[s1] && p.visited[s2]) {
					p.pq.Enqueue(isl, isl.Distance())
				}
			}
		}

		mst = append(mst, link)
		p.visited[newSat] = true
	}

	// Update established status of links
	estSet := make(map[*linkmod.IslLink]bool)
	for _, l := range mst {
		l.SetEstablished(true)
		estSet[l] = true
	}
	for _, l := range p.established {
		if !estSet[l] {
			l.SetEstablished(false)
		}
	}
	p.established = mst

	// Notify other waiters
	select {
	case p.readyCh <- struct{}{}:
	default:
	}

	// Convert to []types.ILink for interface compliance
	result := make([]types.ILink, len(mst))
	for i, l := range mst {
		result[i] = l
	}
	return result, nil
}

// Links returns all known candidate ISL links
func (p *IslSatelliteCentricMstProtocol) Links() []types.ILink {
	p.mu.Lock()
	defer p.mu.Unlock()
	res := make([]types.ILink, len(p.links))
	for i, l := range p.links {
		res[i] = l
	}
	return res
}

// Established returns the currently active ISL links
func (p *IslSatelliteCentricMstProtocol) Established() []types.ILink {
	p.mu.Lock()
	defer p.mu.Unlock()
	res := make([]types.ILink, len(p.established))
	for i, l := range p.established {
		res[i] = l
	}
	return res
}
