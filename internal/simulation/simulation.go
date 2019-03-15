package simulation

import (
	"errors"
	"fmt"
	"log"
	"math"
	"math/rand"
	"time"

	"github.com/decentraland/communications-server-go/internal/authentication"
	"github.com/decentraland/communications-server-go/internal/utils"
	protocol "github.com/decentraland/communications-server-go/pkg/protocol"
	"github.com/golang/protobuf/proto"
	"github.com/segmentio/ksuid"
)

var AVATARS []string = []string{"fox", "round robot", "square robot"}

const (
	PARCEL_SIZE = 16
	MAX_PARCEL  = 150
	MIN_PARCEL  = -150
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
	r := math.Sqrt(math.Pow(float64(v.X), 2) + math.Pow(float64(v.Y), 2) + math.Pow(float64(v.Z), 2))
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
	Auth                      authentication.Authentication
	AuthMethod                string
	Id                        string
	Avatar                    *string
	Checkpoints               []V3
	DurationMs                uint
	SubscribeToPositionTopics bool
}

func updateLocationTopics(client *Client, p V3) {
	radius := 4
	parcelX := int(p.X / PARCEL_SIZE)
	parcelZ := int(p.Z / PARCEL_SIZE)

	minX := ((utils.Max(MIN_PARCEL, parcelX-radius) + MAX_PARCEL) >> 2) << 2
	maxX := ((utils.Min(MAX_PARCEL, parcelX+radius) + MAX_PARCEL) >> 2) << 2
	minZ := ((utils.Max(MIN_PARCEL, parcelZ-radius) + MAX_PARCEL) >> 2) << 2
	maxZ := ((utils.Min(MAX_PARCEL, parcelZ+radius) + MAX_PARCEL) >> 2) << 2

	newTopics := make(map[string]bool)
	topicsChanged := false

	for x := minX; x <= maxX; x += 4 {
		for z := minZ; z <= maxZ; z += 4 {
			hash := fmt.Sprintf("%d:%d", x>>2, z>>2)
			positionTopic := fmt.Sprintf("position:%s", hash)
			profileTopic := fmt.Sprintf("profile:%s", hash)
			chatTopic := fmt.Sprintf("chat:%s", hash)

			newTopics[positionTopic] = true
			newTopics[profileTopic] = true
			newTopics[chatTopic] = true

			if !client.topics[positionTopic] || !client.topics[profileTopic] {
				topicsChanged = true
			}
		}
	}

	if topicsChanged {
		client.topics = newTopics
		client.sendTopicSubscriptionMessage(newTopics)
	}
}

func StartBot(coordinatorUrl string, options BotOptions) {
	if len(options.Checkpoints) < 2 {
		log.Fatal(errors.New("invalid path, need at least two checkpoints"))
	}

	var avatar string

	if options.Avatar != nil {
		avatar = *options.Avatar
	} else {
		avatar = getRandomAvatar()
	}

	peerId := ksuid.New().String()
	url, err := options.Auth.GenerateAuthURL(options.AuthMethod, coordinatorUrl, protocol.Role_CLIENT)
	if err != nil {
		log.Fatal(err)
	}
	client := MakeClient(options.Id, url)

	go func() {
		log.Fatal(client.startCoordination())
	}()

	worldData := <-client.worldData

	log.Println("my alias is", worldData.MyAlias)

	if err := client.connect(worldData.AvailableServers[0]); err != nil {
		log.Fatal(err)
	}

	authMessage, err := options.Auth.GenerateAuthMessage(options.AuthMethod, protocol.Role_CLIENT)
	if err != nil {
		log.Fatal(err)
	}
	bytes, err := proto.Marshal(authMessage)
	if err != nil {
		log.Fatal(err)
	}
	client.authMessage <- bytes
	checkpoints := options.Checkpoints

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
	chatTicker := time.NewTicker(10 * time.Second)
	defer profileTicker.Stop()
	defer positionTicker.Stop()
	defer chatTicker.Stop()

	hashLocation := func() string {
		parcelX := (int(p.X/PARCEL_SIZE) + MAX_PARCEL) >> 2
		parcelZ := (int(p.Z/PARCEL_SIZE) + MAX_PARCEL) >> 2
		hash := fmt.Sprintf("%d:%d", parcelX, parcelZ)
		return hash
	}

	for {
		select {
		case <-profileTicker.C:
			topic := fmt.Sprintf("profile:%s", hashLocation())

			ms := utils.NowMs()
			bytes, err := encodeTopicMessage(topic, &protocol.ProfileData{
				Time:        ms,
				AvatarType:  avatar,
				DisplayName: peerId,
				PublicKey:   "key",
			})
			if err != nil {
				log.Fatal("encode profile failed", err)
			}
			client.sendReliable <- bytes
		case <-chatTicker.C:
			topic := fmt.Sprintf("chat:%s", hashLocation())

			ms := utils.NowMs()
			bytes, err := encodeTopicMessage(topic, &protocol.ChatData{
				Time:      ms,
				MessageId: ksuid.New().String(),
				Text:      "hi",
			})
			if err != nil {
				log.Fatal("encode chat failed", err)
			}
			client.sendReliable <- bytes
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

			if options.SubscribeToPositionTopics {
				updateLocationTopics(client, p)
			}

			topic := fmt.Sprintf("position:%s", hashLocation())
			ms := utils.NowMs()
			bytes, err := encodeTopicMessage(topic, &protocol.PositionData{
				Time:      ms,
				PositionX: float32(p.X),
				PositionY: float32(p.Y),
				PositionZ: float32(p.Z),
				RotationX: 0,
				RotationY: 0,
				RotationZ: 0,
				RotationW: 0,
			})
			if err != nil {
				log.Fatal("encode position failed", err)
			}
			client.sendUnreliable <- bytes
			lastPositionMsg = time.Now()
		}
	}
}
