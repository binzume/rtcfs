
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
仮実装です．roomnameは秘密にする必要があるので適当なランダムっぽい名前にしてください．

```bash
rtcfs -name roomname pairing
```

### ファイルを共有

```bash
rtcfs -name roomname -path /dir/to/share
```

# License

MIT License
