package rtcfs

type ConnectOptions struct {
	SignalingURL string
	SignalingKey string
	RoomID       string

	Password string
}

func (o *ConnectOptions) DefaultRoomID() string {
	return o.RoomID
}
