
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

### ファイルを共有

```bash
rtcfs -room RoomName -path /dir/to/share
```

### ファイルリストを表示(デバッグ用)

```bash
rtcfs -room RoomName ls
```

# License

MIT License
