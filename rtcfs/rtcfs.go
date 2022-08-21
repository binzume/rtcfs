package rtcfs

type ConnectOptions struct {
	SignalingURL string
	SignalingKey string
	RoomID       string

	AuthToken string
}
