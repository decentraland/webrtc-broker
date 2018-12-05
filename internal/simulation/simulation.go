package simulation

import (
	"errors"
	"github.com/decentraland/communications-server-go/internal/worldcomm"
	"github.com/golang/protobuf/proto"
	"github.com/gorilla/websocket"
	"github.com/segmentio/ksuid"
	"log"
	"math"
	"math/rand"
	"time"
)

var AVATARS []string = []string{"fox", "round robot", "square robot"}

const (
	PARCEL_SIZE = 10
 )

func getRandomAvatar() string {
	avatar := AVATARS[rand.Intn(len(AVATARS))]
	return avatar
}

type V3 struct {
	X float64
	Y float64
	Z float64
}

func (v V3) Length() float64 {
	r := math.Sqrt(math.Pow(float64(v.X), 2) + math.Pow(float64(v.Y),2) + math.Pow(float64(v.Z),2))
	return r
}

func (v V3) Sub(a V3) V3 {
	return V3{v.X - a.X, v.Y - a.Y, v.Z - a.Z}
}

func (v V3) Add(a V3) V3 {
	return V3{v.X + a.X, v.Y + a.Y, v.Z + a.Z}
}

func (v V3) ScalarProd(n float64) V3 {
	return V3{v.X * n, v.Y * n, v.Z * n}
}

func (v V3) Normalize() V3 {
	len := v.Length()
	return v.ScalarProd(1 / len)
}

type BotOptions struct {
	CommServerUrl string
	Avatar        *string
	Checkpoints   []V3
	DurationMs    uint32
}

type Bot struct {
	peerId string
}

func StartBot(options BotOptions) (Bot, error) {
	if len(options.Checkpoints) < 2 {
		return Bot{}, errors.New("invalid path, need at least two checkpoints")
	}

	var avatar string

	if options.Avatar != nil {
		avatar = *options.Avatar
	} else {
		avatar = getRandomAvatar()
	}

	peerId := ksuid.New().String()
	bot := Bot{peerId: peerId}

	checkpoints := options.Checkpoints

	go func() {
		c, _, err := websocket.DefaultDialer.Dial(options.CommServerUrl, nil)
		if err != nil {
			log.Fatal("dial:", err)
		}

		defer c.Close()

		totalDistance := 0.0
		for i := 1; i < len(checkpoints); i += 1 {
			totalDistance += checkpoints[i].Sub(checkpoints[i-1]).Length()
		}

		// NOTE: velocity in ms
		vMs := totalDistance / float64(options.DurationMs)

		p := checkpoints[0]
		nextCheckpointIndex := 1
		lastPositionMsg := time.Now()

		profileTicker := time.NewTicker(1 * time.Second)
		positionTicker := time.NewTicker(100 * time.Millisecond)
		defer profileTicker.Stop()
		defer positionTicker.Stop()

		go func() {
			for {
				_, message, err := c.ReadMessage()
				if err != nil {
					log.Println("read:", err)
					return
				}
				log.Printf("recv: %s", message)
			}
		}()

		for {

			select {
			case <-profileTicker.C:
				_, ms := worldcomm.Now()
				msg := &worldcomm.ProfileMessage{
					Type:        worldcomm.MessageType_PROFILE,
					Time:        ms,
					PositionX:   float32(p.X / PARCEL_SIZE),
					PositionZ:   float32(p.Z / PARCEL_SIZE),
					PeerId:      peerId,
					AvatarType:  avatar,
					DisplayName: peerId,
				}

				bytes, err := proto.Marshal(msg)
				if err != nil {
					log.Fatal("encode profile failed", err)
				}
				if err := c.WriteMessage(websocket.BinaryMessage, bytes); err != nil {
					log.Fatal("error writing profile message to socket", err)
				}
			case <-positionTicker.C:
				nextCheckpoint := checkpoints[nextCheckpointIndex]
				v := nextCheckpoint.Sub(p)
				tMax := float64(v.Length()) / vMs
				dt := float64(time.Since(lastPositionMsg).Nanoseconds() / int64(time.Millisecond))

				if dt < tMax {
					dir := v.Normalize()
					p = p.Add(dir.ScalarProd(dt * vMs))
				} else {
					if nextCheckpointIndex == len(checkpoints)-1 {
						nextCheckpointIndex = 0
					} else {
						nextCheckpointIndex += 1
					}
					p = nextCheckpoint
				}

				_, ms := worldcomm.Now()
				msg := &worldcomm.PositionMessage{
					Type:      worldcomm.MessageType_POSITION,
					Time:      ms,
					PositionX: float32(p.X / PARCEL_SIZE),
					PositionY: float32(p.Y),
					PositionZ: float32(p.Z / PARCEL_SIZE),
					RotationX: 0,
					RotationY: 0,
					RotationZ: 0,
					RotationW: 0,
				}

				bytes, err := proto.Marshal(msg)
				if err != nil {
					log.Fatal("encode position failed", err)
				}
				if err := c.WriteMessage(websocket.BinaryMessage, bytes); err != nil {
					log.Fatal("error writing position message to socket", err)
				}
				lastPositionMsg = time.Now()
			}
		}
	}()

	return bot, nil
}
