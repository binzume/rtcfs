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

### セキュリティ

`RoomName` はWebRTCのシグナリングサーバを経由するので自身の管理下にないサーバを使う場合に第三者が知る可能性があります．
気休めとして `-passwd` オプションで認証を行うためのパスワードを指定できます．
パスワードはWebRTCのデータチャンネル上でDTSL証明書のフィンガープリントの検証に使い、接続相手にも直接は送信されないので安全にWebRTCのEnd-to-end encryptionを確認できます(pairing機能利用時は除く)

# License

MIT License
