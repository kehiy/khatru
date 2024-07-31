package khatru

import (
	"context"
	"errors"

	"github.com/nbd-wtf/go-nostr"
)

var ErrSubscriptionClosedByClient = errors.New("subscription closed by client")

type listenerSpec struct {
	subscriptionId string // kept here so we can easily match against it removeListenerId
	cancel         context.CancelCauseFunc
	index          int
	subrelay       *Relay // this is important when we're dealing with routing, otherwise it will be always the same
}

type listener struct {
	subscriptionId string // duplicated here so we can easily send it on notifyListeners
	filter         nostr.Filter
	ws             *WebSocket
}

func (rl *Relay) GetListeningFilters() []nostr.Filter {
	respfilters := make([]nostr.Filter, len(rl.listeners))
	for i, l := range rl.listeners {
		respfilters[i] = l.filter
	}
	return respfilters
}

// addListener may be called multiple times for each id and ws -- in which case each filter will
// be added as an independent listener
func (rl *Relay) addListener(
	ws *WebSocket,
	id string,
	subrelay *Relay,
	filter nostr.Filter,
	cancel context.CancelCauseFunc,
) {
	rl.clientsMutex.Lock()
	defer rl.clientsMutex.Unlock()

	if specs, ok := rl.clients[ws]; ok /* this will always be true unless client has disconnected very rapidly */ {
		idx := len(subrelay.listeners)
		rl.clients[ws] = append(specs, listenerSpec{
			subscriptionId: id,
			cancel:         cancel,
			subrelay:       subrelay,
			index:          idx,
		})
		subrelay.listeners = append(subrelay.listeners, listener{
			ws:             ws,
			subscriptionId: id,
			filter:         filter,
		})
	}
}

// remove a specific subscription id from listeners for a given ws client
// and cancel its specific context
func (rl *Relay) removeListenerId(ws *WebSocket, id string) {
	rl.clientsMutex.Lock()
	defer rl.clientsMutex.Unlock()

	if specs, ok := rl.clients[ws]; ok {
		// swap delete specs that match this id
		nswaps := 0
		for s, spec := range specs {
			if spec.subscriptionId == id {
				spec.cancel(ErrSubscriptionClosedByClient)
				specs[s] = specs[len(specs)-1-nswaps]
				nswaps++

				// swap delete listeners one at a time, as they may be each in a different subrelay
				srl := spec.subrelay // == rl in normal cases, but different when this came from a route
				srl.listeners[spec.index] = srl.listeners[len(srl.listeners)-1]
				srl.listeners = srl.listeners[0 : len(srl.listeners)-1]
			}
		}
		rl.clients[ws] = specs[0 : len(specs)-nswaps]
	}
}

func (rl *Relay) notifyListeners(event *nostr.Event) {
	for _, listener := range rl.listeners {
		if listener.filter.Matches(event) {
			for _, pb := range rl.PreventBroadcast {
				if pb(listener.ws, event) {
					return
				}
			}
			listener.ws.WriteJSON(nostr.EventEnvelope{SubscriptionID: &listener.subscriptionId, Event: *event})
		}
	}
}
