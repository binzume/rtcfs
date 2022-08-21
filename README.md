
# WIP: WebRTC fs

WebRTC の DataCnannel 上でファイルを共有するツールです．プロトコルは， https://github.com/binzume/webrtc-rdp 等と共通．

## Usage

Go 1.18以降が必要です．

### インストール

```bash
go install github.com/binzume/webrtcfs@latest
```

### ファイルを共有

```bash
webrtcfs -room RoomName -path /dir/to/share
```

### クライアント

とりあえずデバッグ用に作った簡易的なシェルが付いています．

TODO: FUSE support.


```bash
# start shell
webrtcfs -room RoomName shell

# file list
webrtcfs -room RoomName ls /
# traverse directories
webrtcfs -room RoomName ls /**

# copy remote to local
webrtcfs -room RoomName pull remotefile.txt
# copy local to remote
webrtcfs -room RoomName push localfile.txt
```


### ペアリング

https://github.com/binzume/webrtc-rdp から接続するためのPINを生成します．

PINが表示されるのでクライアント側に入力してください．
仮実装です．RoomNameは秘密にする必要があるので適当なランダムっぽい名前にしてください．

```bash
webrtcfs -room RoomName pairing
```

RoomNameはWebRTCのシグナリングサーバを経由してしまうので，気休めとして `-token` オプションで追加のパスワードを設定できます．


# License

MIT License
