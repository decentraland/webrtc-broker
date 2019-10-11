// Package simulation contains utilities for simulating clients
package simulation

import (
	"time"

	"github.com/rs/zerolog"

	"github.com/decentraland/webrtc-broker/pkg/authentication"
	"github.com/decentraland/webrtc-broker/pkg/protocol"
	"github.com/golang/protobuf/proto"

	pion "github.com/pion/webrtc/v2"
)

// BotOptions ...
type BotOptions struct {
	CoordinatorURL string
	Topic          string
	Subscription   map[string]bool
	TrackStats     bool
	Log            zerolog.Logger
}

// StartBot ...
func StartBot(opts BotOptions) {
	log := opts.Log
	config := Config{
		Auth:           &authentication.NoopAuthenticator{},
		CoordinatorURL: opts.CoordinatorURL,
		ICEServers: []pion.ICEServer{
			{
				URLs: []string{"stun:stun.l.google.com:19302"},
			},
		},
		Log: log,
	}

	if opts.TrackStats {
		trackCh := make(chan []byte, 256)
		config.OnMessageReceived = func(reliable bool, msgType protocol.MessageType, raw []byte) {
			trackCh <- raw
		}

		go func() {
			peers := make(map[uint64]*Stats)
			topicFwMessage := protocol.TopicFWMessage{}

			onMessage := func(rawMsg []byte) {
				if err := proto.Unmarshal(rawMsg, &topicFwMessage); err != nil {
					log.Error().Err(err).Msg("error unmarshalling data message")
					return
				}

				alias := topicFwMessage.FromAlias
				stats := peers[alias]

				if stats == nil {
					stats = &Stats{}
					peers[alias] = stats
				}

				stats.Seen(time.Now())
			}

			reportTicker := time.NewTicker(30 * time.Second)
			defer reportTicker.Stop()

			for {
				select {
				case rawMsg := <-trackCh:
					onMessage(rawMsg)

					n := len(trackCh)
					for i := 0; i < n; i++ {
						rawMsg = <-trackCh
						onMessage(rawMsg)
					}
				case <-reportTicker.C:
					for alias, stats := range peers {
						log.Info().Msgf("%d: %f ms (%d messages)", alias, stats.Avg(), stats.Samples())

						if time.Since(stats.LastSeen).Seconds() > 1 {
							delete(peers, alias)
						}
					}
				}
			}
		}()
	}

	client := Start(&config)

	err := client.SendTopicSubscriptionMessage(opts.Subscription)
	if err != nil {
		log.Fatal().Err(err).Msg("sending topic subscription message")
	}

	if opts.Topic != "" {
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()

		for {
			<-ticker.C

			msg := &protocol.TopicMessage{
				Type:  protocol.MessageType_TOPIC,
				Topic: opts.Topic,
				Body:  make([]byte, 9),
			}

			bytes, err := proto.Marshal(msg)
			if err != nil {
				log.Fatal().Err(err).Msg("encode message failed")
			}

			client.SendUnreliable <- bytes
		}
	}
}
