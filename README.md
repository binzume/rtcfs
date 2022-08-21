
# WIP: WebRTC fs

WebRTC の DataCnannel 上でファイルを共有するツールです．クライアントは， https://github.com/binzume/webrtc-rdp 等．

## Usage

Go 1.18以降が必要です．

### インストール

```bash
go install github.com/binzume/rtcfs@latest
```

### ペアリング

PINが表示されるのでクライアント側に入力してください．
仮実装です．RoomNameは秘密にする必要があるので適当なランダムっぽい名前にしてください．

```bash
rtcfs -room RoomName pairing
```

RoomNameはWebRTCのシグナリングサーバを経由してしまうので，気休めとして `-token` オプションで追加のパスワードを設定できます．

### ファイルを共有

```bash
rtcfs -room RoomName -path /dir/to/share
```

### クライアント

とりあえずデバッグ用に作った簡易的なシェルが付いています．

TODO: FUSE support.


```bash
# start shell
rtcfs -room RoomName shell

# file list
rtcfs -room RoomName ls /
# traverse directories
rtcfs -room RoomName ls /**

# copy remote to local
rtcfs -room RoomName pull remotefile.txt
# copy local to remote
rtcfs -room RoomName push localfile.txt
```

# License

MIT License
