package client

import (
	"errors"
	"fmt"
	"reflect"
	"strings"

	"euphoria.io/heim/proto"
)

var (
	errNotHandled = errors.New("not handled")

	mapping = map[proto.PacketType]reflect.Type{
		proto.BounceEventType:      reflect.TypeOf(&proto.BounceEvent{}),
		proto.DisconnectEventType:  reflect.TypeOf(&proto.DisconnectEvent{}),
		proto.EditMessageEventType: reflect.TypeOf(&proto.EditMessageEvent{}),
		proto.HelloEventType:       reflect.TypeOf(&proto.HelloEvent{}),
		proto.JoinEventType:        reflect.TypeOf(&proto.PresenceEvent{}),
		proto.NetworkEventType:     reflect.TypeOf(&proto.NetworkEvent{}),
		proto.NickEventType:        reflect.TypeOf(&proto.NickEvent{}),
		proto.PartEventType:        reflect.TypeOf(&proto.PresenceEvent{}),
		proto.PingEventType:        reflect.TypeOf(&proto.PingEvent{}),
		proto.SendEventType:        reflect.TypeOf(&proto.SendEvent{}),
		proto.SendReplyType:        reflect.TypeOf(&proto.SendReply{}),
		proto.SnapshotEventType:    reflect.TypeOf(&proto.SnapshotEvent{}),
	}
)

func methodName(packetType proto.PacketType) string {
	parts := strings.Split(string(packetType), "-")
	for i := range parts {
		parts[i] = strings.ToUpper(parts[i][0:1]) + parts[i][1:]
	}
	return strings.Join(parts, "")
}

type FeatureSet struct {
	f map[proto.PacketType][]reflect.Value
}

func (fs *FeatureSet) Add(handler interface{}) {
	if fs.f == nil {
		fs.f = map[proto.PacketType][]reflect.Value{}
	}
	handlerValue := reflect.ValueOf(handler)
	for packetType, eventType := range mapping {
		method := handlerValue.MethodByName(methodName(packetType))
		if method.IsValid() {
			methodType := method.Type()
			if methodType.NumIn() == 1 && methodType.In(0) == eventType {
				fs.f[packetType] = append(fs.f[packetType], method)
			}
		}
	}
}

func (fs *FeatureSet) Handle(packet *proto.Packet) error {
	payload, err := packet.Payload()
	if err != nil {
		return err
	}

	handled := false
	args := []reflect.Value{reflect.ValueOf(payload)}
	for _, handler := range fs.f[packet.Type] {
		go func(handler reflect.Value) {
			result := handler.Call(args)
			if !result[0].IsNil() {
				fmt.Printf("error: %s\n", result[0].Interface())
			}
		}(handler)
		handled = true
	}
	if !handled {
		return errNotHandled
	}
	return nil
}
