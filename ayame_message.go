package main

type RegisterMessage struct {
	Type          string      `json:"type"`
	RoomID        string      `json:"roomId"`
	ClientID      string      `json:"clientId,omitempty"`
	AuthnMetadata interface{} `json:"authnMetadata,omitempty"`
	SignalingKey  string      `json:"signalingKey,omitempty"`
}

type AuthResultMessage struct {
	Type          string       `json:"type"` // accept, reject
	IsExistClient bool         `json:"isExistClient"`
	AuthzMetadata interface{}  `json:"authzMetadata,omitempty"`
	IceServers    []*IceServer `json:"iceServers,omitempty"`
	Reason        string       `json:"reason"`
}

type IceServer struct {
	URLs       []string `json:"urls"`
	UserName   *string  `json:"username,omitempty"`
	Credential *string  `json:"credential,omitempty"`
}

type SignalingMessage struct {
	Type string `json:"type"`
	SDP  string `json:"sdp"` // offer, answer
	ICE  struct {
		Candidate string `json:"candidate"` // candidate
	}
}

type PongMessage struct {
	Type string `json:"type"`
}

type ByeMessage struct {
	Type string `json:"type"`
}
