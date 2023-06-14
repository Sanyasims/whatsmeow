module go.mau.fi/whatsmeow/mdtest

go 1.20

require (
	github.com/mattn/go-sqlite3 v1.14.17
	github.com/mdp/qrterminal/v3 v3.1.1
	go.mau.fi/whatsmeow v0.0.0-20230614142319-2114a3c181bd
	google.golang.org/protobuf v1.30.0
)

require (
	filippo.io/edwards25519 v1.0.0 // indirect
	github.com/gorilla/websocket v1.5.0 // indirect
	go.mau.fi/libsignal v0.1.0 // indirect
	golang.org/x/crypto v0.10.0 // indirect
	rsc.io/qr v0.2.0 // indirect
)

replace go.mau.fi/whatsmeow => ../
