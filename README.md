
# WebRTC fs

WebRTC の DataCnannel 上でファイルを共有する実験的なファイルシステムです．

プロトコルは， https://github.com/binzume/webrtc-rdp 等と共通です．
PCで共有したフォルダをブラウザで表示したり，ブラウザで共有したフォルダをドライブとしてマウントしたりできます．

## Usage

Go 1.19以降が必要です．

### インストール

```bash
go install github.com/binzume/webrtcfs@latest
```

### ファイルを共有

```bash
webrtcfs -room RoomName publish /dir/to/share
```

### クライアント

とりあえずデバッグ用に作った簡易的なシェルが付いています．


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

FUSEでマウントする場合．

```bash
go install github.com/binzume/webrtcfs/cmds/mount_webrtcfs@latest
mount_webrtcfs -room RoomName R:
```

`R:` ドライブが追加されて，エクスプローラーなどでアクセスできるようになります．
Windows以外ではマウントポイントとして使う適当なディレクトリを指定してください．

### ペアリング

https://github.com/binzume/webrtc-rdp から接続するためのPINを生成します．

PINが表示されるのでクライアント側に入力してください．

```bash
webrtcfs -room RoomName pairing
```

RoomNameは秘密にする必要があるので適当なランダムっぽい名前にしてください．
RoomNameはWebRTCのシグナリングサーバを経由してしまうので，気休めとして `-token` オプションでデータチャンネル上で認証を行うためのパスワードを指定できます．

# License

MIT License
