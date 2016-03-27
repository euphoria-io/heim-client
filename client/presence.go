package client

import (
	"fmt"
	"sort"
	"strings"

	"euphoria.io/heim/proto"
)

type Presence struct {
	listing proto.Listing
}

func (p *Presence) SnapshotEvent(event *proto.SnapshotEvent) error {
	p.listing = make(proto.Listing, len(event.Listing))
	copy(p.listing, event.Listing)
	sort.Sort(p.listing)
	p.PrintWho()
	return nil
}

func (p *Presence) JoinEvent(event *proto.PresenceEvent) error {
	p.listing = append(p.listing, proto.SessionView(*event))
	sort.Sort(p.listing)
	return nil
}

func (p *Presence) NickEvent(event *proto.NickEvent) error {
	if event.From == "" {
		// TODO: make terminal-safe
		fmt.Printf("* %s has joined the room\n", event.To)
	} else {
		// TODO: make terminal-safe
		fmt.Printf("* %s is now known as %s\n", event.From, event.To)
	}
	for i, session := range p.listing {
		if session.SessionID == event.SessionID {
			p.listing[i].Name = event.To
		}
	}
	sort.Sort(p.listing)
	return nil
}

func (p *Presence) PrintWho() {
	lurkers := 0
	names := make([]string, 0, len(p.listing))
	for _, session := range p.listing {
		if session.Name == "" {
			lurkers++
		} else {
			names = append(names, session.Name)
		}
	}
	fmt.Printf("%s\n", strings.Join(names, ", "))
	if lurkers == 1 {
		fmt.Printf("  and one lurker\n")
	} else if lurkers > 1 {
		fmt.Printf("  and %d lurkers\n", lurkers)
	}
}
