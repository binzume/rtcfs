package main

type EmptyMessage struct {
	Type string `json:"type"`
}

type RegisterMessage struct {
	Type          string      `json:"type"` // register
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
	URLs       []string    `json:"urls"`
	Username   string      `json:"username,omitempty"`
	Credential interface{} `json:"credential,omitempty"`
}

type SignalingMessage struct {
	Type string        `json:"type"`          // offer, answer, candidate
	SDP  string        `json:"sdp,omitempty"` // offer, answer
	ICE  *ICECandidate `json:"ice,omitempty"` // candidate
}

type ICECandidate struct {
	Candidate     string  `json:"candidate"`
	SdpMid        *string `json:"sdpMid,omitempty"`
	SdpMLineIndex *uint16 `json:"sdpMLineIndex,omitempty"`
}
