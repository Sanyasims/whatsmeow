package wainstance

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"go.mau.fi/whatsmeow/socket"
	"go.mau.fi/whatsmeow/webtest/properties"
	"go.mau.fi/whatsmeow/webtest/webhook"
	"go.mau.fi/whatsmeow/webtest/ws"
	"mime"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"google.golang.org/protobuf/proto"

	qrcode "github.com/skip2/go-qrcode"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/appstate"
	waProto "go.mau.fi/whatsmeow/binary/proto"
	"go.mau.fi/whatsmeow/store"
	"go.mau.fi/whatsmeow/store/sqlstore"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	waLog "go.mau.fi/whatsmeow/util/log"
)

var InstanceWa Instance

type Instance struct {
	Client                        *whatsmeow.Client
	Log                           waLog.Logger
	DbLog                         waLog.Logger
	DebugLogs                     *bool
	DbDialect                     *string
	DbAddress                     *string
	RequestFullSync               *bool
	PairRejectChan                chan bool
	HistorySyncID                 int32
	StartupTime                   int64
	Config                        properties.Configuration
	WebhookUrl                    string
	WsQrClient                    *ws.ClientWs
	ChainResponseGetStatusAccount chan properties.ResponseGetStatusAccount
}

// StartInstance Метод запускает инстанс
func StartInstance(proxy socket.Proxy, onlyIfAuth bool) {

	// создаем хранилище инстанса
	storeContainer, err := sqlstore.New(*InstanceWa.DbDialect, *InstanceWa.DbAddress, InstanceWa.DbLog)

	// если ошибка
	if err != nil {

		// выводим ошибку
		InstanceWa.Log.Errorf("Failed to connect to database: %v", err)

		// не продолжаем
		return
	}

	// получаем данные устройства
	device, err := storeContainer.GetFirstDevice()

	// если ощшибка
	if err != nil {

		// выводим ошибку
		InstanceWa.Log.Errorf("Failed to get device: %v", err)

		// не продолжаем
		return
	}

	if (device.ID == nil || device.ID.User == "") && onlyIfAuth {

		// выводим ошибку
		InstanceWa.Log.Infof("Instance not authorized")

		// не продолжаем
		return
	}

	// создаем клиента
	InstanceWa.Client = whatsmeow.NewClient(device, waLog.Stdout("ClientWs", "DEBUG", true))

	//передаем ссылку на WsClient
	InstanceWa.Client.WsQrClient = InstanceWa.WsQrClient

	var isWaitingForPair atomic.Bool

	InstanceWa.Client.PrePairCallback = func(jid types.JID, platform, businessName string) bool {

		isWaitingForPair.Store(true)

		defer isWaitingForPair.Store(false)

		InstanceWa.Log.Infof("Pairing %s (platform: %q, business name: %q). Type r within 3 seconds to reject pair", jid, platform, businessName)

		select {
		case reject := <-InstanceWa.PairRejectChan:

			if reject {

				InstanceWa.Log.Infof("Rejecting pair")

				return false
			}
		case <-time.After(3 * time.Second):
		}

		InstanceWa.Log.Infof("Accepting pair")

		return true
	}

	ch, err := InstanceWa.Client.GetQRChannel(context.Background())

	if err != nil {

		// если аккаунт авторизован
		if device.ID.User != "" {

			// создаем структу ws сообщения
			InstanceWa.Client.AuthMessage = &ws.AuthMessage{
				Type:   "error",
				Reason: "Instance already authorized",
			}

			// если есть сокет сообщение
			if InstanceWa.WsQrClient != nil && InstanceWa.WsQrClient.Socket != nil {

				// отправляем QR код в ws
				if !InstanceWa.WsQrClient.Send(*InstanceWa.Client.AuthMessage) {

					// выводим ошибку
					InstanceWa.Log.Errorf("Error send QR code to websocket")
				}

				// закрываем сокет соединение
				InstanceWa.WsQrClient.Close()
			}
		}

		// This error means that we're already logged in, so ignore it.
		if !errors.Is(err, whatsmeow.ErrQRStoreContainsID) {

			InstanceWa.Log.Errorf("Failed to get QR channel: %v", err)
		}
	} else {

		go func() {

			for evt := range ch {

				if evt.Event == "code" {

					// создаем буфер для байт QR кода
					var png []byte

					// создаем изображение QR кода
					png, err := qrcode.Encode(evt.Code, qrcode.Medium, 264)

					// если ошибка
					if err != nil {

						// выводим ошитбку
						InstanceWa.Log.Errorf("QR string: %v", err)

						// не продолжаем
						return
					}

					// создаем структу ws сообщения
					InstanceWa.Client.AuthMessage = &ws.AuthMessage{
						Type:        "qr",
						ImageQrCode: "data:image/png;base64, " + base64.StdEncoding.EncodeToString(png),
					}

					// если инциализировано сокет соединение
					if InstanceWa.WsQrClient != nil {

						// отправляем QR код в ws
						if !InstanceWa.WsQrClient.Send(*InstanceWa.Client.AuthMessage) {

							// выводим ошитбку
							InstanceWa.Log.Errorf("Error send QR code to websocket")

							// не продолжаем
							return
						}
					}

				} else if evt.Event == "timeout" {

					// создаем структу ws сообщения
					InstanceWa.Client.AuthMessage = &ws.AuthMessage{
						Type:   "error",
						Reason: "QR code was not scanned in the required time",
					}

					// если инциализировано сокет соединение
					if InstanceWa.WsQrClient != nil {

						// отправляем QR код в ws
						if !InstanceWa.WsQrClient.Send(*InstanceWa.Client.AuthMessage) {

							// выводим ошибку
							InstanceWa.Log.Errorf("Error send QR code to websocket")

							// не продолжаем
							return
						}

						// закрываем сокет соединение
						InstanceWa.WsQrClient.Close()
					}

				} else {

					// выводим лог
					InstanceWa.Log.Infof("QR channel result: %s", evt.Event)
				}
			}
		}()
	}

	InstanceWa.Client.AddEventHandler(handler)

	// устанавливаем прокси
	InstanceWa.Client.SetProxy(proxy)

	err = InstanceWa.Client.Connect()

	if err != nil {

		InstanceWa.Log.Errorf("Failed to connect: %v", err)

		return
	}

	c := make(chan os.Signal)

	input := make(chan string)

	signal.Notify(c, os.Interrupt, syscall.SIGTERM)

	go func() {

		defer close(input)

		scan := bufio.NewScanner(os.Stdin)

		for scan.Scan() {

			line := strings.TrimSpace(scan.Text())

			if len(line) > 0 {

				input <- line
			}
		}
	}()

	for {
		select {
		case <-c:

			InstanceWa.Log.Infof("Interrupt received, exiting")

			InstanceWa.Client.Disconnect()

			return
		case cmd := <-input:

			if len(cmd) == 0 {

				//log.Infof("Stdin closed, exiting")
				//
				//cli.Disconnect()

				return
			}
			if isWaitingForPair.Load() {

				if cmd == "r" {

					InstanceWa.PairRejectChan <- true

				} else if cmd == "a" {

					InstanceWa.PairRejectChan <- false
				}

				continue
			}

			args := strings.Fields(cmd)

			cmd = args[0]

			args = args[1:]

			go handleCmd(strings.ToLower(cmd), args)
		}
	}
}

// StopInstance Метод останавливает инстанс
func StopInstance() {

	if InstanceWa.Client == nil {
		return
	}

	InstanceWa.Client.Disconnect()
}

// SetPresence устанавливает доступность себя
func SetPresence(presence string) error {
	return InstanceWa.Client.SendPresence(types.Presence(presence))
}

// SubscribePresence метод подписывается на пользователя
func SubscribePresence(phone string) error {
	jid, ok := ParseJID(phone)
	if !ok {
		return errors.New("error parse JID")
	}
	return InstanceWa.Client.SubscribePresence(jid)
}

// GetUser метод отдает данные пользователя
func GetUser(phone string) (map[types.JID]types.UserInfo, error) {

	jid, ok := ParseJID(phone)

	if !ok {
		return nil, errors.New("error parse JID")
	}

	return InstanceWa.Client.GetUserInfo([]types.JID{jid})
}

// ParseJID Метод парсит идентификатор Whatsapp
func ParseJID(arg string) (types.JID, bool) {
	if arg[0] == '+' {
		arg = arg[1:]
	}
	if !strings.ContainsRune(arg, '@') {
		return types.NewJID(arg, types.DefaultUserServer), true
	} else {
		recipient, err := types.ParseJID(arg)
		if err != nil {
			InstanceWa.Log.Errorf("Invalid JID %s: %v", arg, err)
			return recipient, false
		} else if recipient.User == "" {
			InstanceWa.Log.Errorf("Invalid JID %s: no server specified", arg)
			return recipient, false
		}
		return recipient, true
	}
}

// Метод обрабатывает команду
func handleCmd(cmd string, args []string) {
	switch cmd {
	case "reconnect":
		InstanceWa.Client.Disconnect()
		err := InstanceWa.Client.Connect()
		if err != nil {
			InstanceWa.Log.Errorf("Failed to connect: %v", err)
		}
	case "logout": //Сделал в API
		err := InstanceWa.Client.Logout()
		if err != nil {
			InstanceWa.Log.Errorf("Error logging out: %v", err)
		} else {
			InstanceWa.Log.Infof("Successfully logged out")
		}
	case "appstate":
		if len(args) < 1 {
			InstanceWa.Log.Errorf("Usage: appstate <types...>")
			return
		}
		names := []appstate.WAPatchName{appstate.WAPatchName(args[0])}
		if args[0] == "all" {
			names = []appstate.WAPatchName{appstate.WAPatchRegular, appstate.WAPatchRegularHigh, appstate.WAPatchRegularLow, appstate.WAPatchCriticalUnblockLow, appstate.WAPatchCriticalBlock}
		}
		resync := len(args) > 1 && args[1] == "resync"
		for _, name := range names {
			err := InstanceWa.Client.FetchAppState(name, resync, false)
			if err != nil {
				InstanceWa.Log.Errorf("Failed to sync app state: %v", err)
			}
		}
	case "request-appstate-key":
		if len(args) < 1 {
			InstanceWa.Log.Errorf("Usage: request-appstate-key <ids...>")
			return
		}
		var keyIDs = make([][]byte, len(args))
		for i, id := range args {
			decoded, err := hex.DecodeString(id)
			if err != nil {
				InstanceWa.Log.Errorf("Failed to decode %s as hex: %v", id, err)
				return
			}
			keyIDs[i] = decoded
		}
		InstanceWa.Client.DangerousInternals().RequestAppStateKeys(context.Background(), keyIDs)
	case "checkuser": //Сделал в API
		if len(args) < 1 {
			InstanceWa.Log.Errorf("Usage: checkuser <phone numbers...>")
			return
		}
		resp, err := InstanceWa.Client.IsOnWhatsApp(args)
		if err != nil {
			InstanceWa.Log.Errorf("Failed to check if users are on WhatsApp:", err)
		} else {
			for _, item := range resp {
				if item.VerifiedName != nil {
					InstanceWa.Log.Infof("%s: on whatsapp: %t, JID: %s, business name: %s", item.Query, item.IsIn, item.JID, item.VerifiedName.Details.GetVerifiedName())
				} else {
					InstanceWa.Log.Infof("%s: on whatsapp: %t, JID: %s", item.Query, item.IsIn, item.JID)
				}
			}
		}
	case "checkupdate":
		resp, err := InstanceWa.Client.CheckUpdate()
		if err != nil {
			InstanceWa.Log.Errorf("Failed to check for updates: %v", err)
		} else {
			InstanceWa.Log.Debugf("Version data: %#v", resp)
			if resp.ParsedVersion == store.GetWAVersion() {
				InstanceWa.Log.Infof("ClientWs is up to date")
			} else if store.GetWAVersion().LessThan(resp.ParsedVersion) {
				InstanceWa.Log.Warnf("ClientWs is outdated")
			} else {
				InstanceWa.Log.Infof("ClientWs is newer than latest")
			}
		}
	case "subscribepresence": //Сделал в API
		if len(args) < 1 {
			InstanceWa.Log.Errorf("Usage: subscribepresence <jid>")
			return
		}
		jid, ok := ParseJID(args[0])
		if !ok {
			return
		}
		err := InstanceWa.Client.SubscribePresence(jid)
		if err != nil {
			fmt.Println(err)
		}
	case "presence": //Сделал в API
		if len(args) == 0 {
			InstanceWa.Log.Errorf("Usage: presence <available/unavailable>")
			return
		}
		fmt.Println(InstanceWa.Client.SendPresence(types.Presence(args[0])))
	case "chatpresence":
		if len(args) == 2 {
			args = append(args, "")
		} else if len(args) < 2 {
			InstanceWa.Log.Errorf("Usage: chatpresence <jid> <composing/paused> [audio]")
			return
		}
		jid, _ := types.ParseJID(args[0])
		fmt.Println(InstanceWa.Client.SendChatPresence(jid, types.ChatPresence(args[1]), types.ChatPresenceMedia(args[2])))
	case "privacysettings":
		resp, err := InstanceWa.Client.TryFetchPrivacySettings(false)
		if err != nil {
			fmt.Println(err)
		} else {
			fmt.Printf("%+v\n", resp)
		}
	case "getuser": //Сделал в API
		if len(args) < 1 {
			InstanceWa.Log.Errorf("Usage: getuser <jids...>")
			return
		}
		var jids []types.JID
		for _, arg := range args {
			jid, ok := ParseJID(arg)
			if !ok {
				return
			}
			jids = append(jids, jid)
		}
		resp, err := InstanceWa.Client.GetUserInfo(jids)
		if err != nil {
			InstanceWa.Log.Errorf("Failed to get user info: %v", err)
		} else {
			for jid, info := range resp {
				InstanceWa.Log.Infof("%s: %+v", jid, info)
			}
		}
	case "mediaconn":
		conn, err := InstanceWa.Client.DangerousInternals().RefreshMediaConn(false)
		if err != nil {
			InstanceWa.Log.Errorf("Failed to get media connection: %v", err)
		} else {
			InstanceWa.Log.Infof("Media connection: %+v", conn)
		}
	case "getavatar": //Сделал в API
		if len(args) < 1 {
			InstanceWa.Log.Errorf("Usage: getavatar <jid> [existing ID] [--preview] [--community]")
			return
		}
		jid, ok := ParseJID(args[0])
		if !ok {
			return
		}
		existingID := ""
		if len(args) > 2 {
			existingID = args[2]
		}
		var preview, isCommunity bool
		for _, arg := range args {
			if arg == "--preview" {
				preview = true
			} else if arg == "--community" {
				isCommunity = true
			}
		}
		pic, err := InstanceWa.Client.GetProfilePictureInfo(jid, &whatsmeow.GetProfilePictureParams{
			Preview:     preview,
			IsCommunity: isCommunity,
			ExistingID:  existingID,
		})
		if err != nil {
			InstanceWa.Log.Errorf("Failed to get avatar: %v", err)
		} else if pic != nil {
			InstanceWa.Log.Infof("Got avatar ID %s: %s", pic.ID, pic.URL)
		} else {
			InstanceWa.Log.Infof("No avatar found")
		}
	case "getgroup":
		if len(args) < 1 {
			InstanceWa.Log.Errorf("Usage: getgroup <jid>")
			return
		}
		group, ok := ParseJID(args[0])
		if !ok {
			return
		} else if group.Server != types.GroupServer {
			InstanceWa.Log.Errorf("Input must be a group JID (@%s)", types.GroupServer)
			return
		}
		resp, err := InstanceWa.Client.GetGroupInfo(group)
		if err != nil {
			InstanceWa.Log.Errorf("Failed to get group info: %v", err)
		} else {
			InstanceWa.Log.Infof("Group info: %+v", resp)
		}
	case "subgroups":
		if len(args) < 1 {
			InstanceWa.Log.Errorf("Usage: subgroups <jid>")
			return
		}
		group, ok := ParseJID(args[0])
		if !ok {
			return
		} else if group.Server != types.GroupServer {
			InstanceWa.Log.Errorf("Input must be a group JID (@%s)", types.GroupServer)
			return
		}
		resp, err := InstanceWa.Client.GetSubGroups(group)
		if err != nil {
			InstanceWa.Log.Errorf("Failed to get subgroups: %v", err)
		} else {
			for _, sub := range resp {
				InstanceWa.Log.Infof("Subgroup: %+v", sub)
			}
		}
	case "communityparticipants":
		if len(args) < 1 {
			InstanceWa.Log.Errorf("Usage: communityparticipants <jid>")
			return
		}
		group, ok := ParseJID(args[0])
		if !ok {
			return
		} else if group.Server != types.GroupServer {
			InstanceWa.Log.Errorf("Input must be a group JID (@%s)", types.GroupServer)
			return
		}
		resp, err := InstanceWa.Client.GetLinkedGroupsParticipants(group)
		if err != nil {
			InstanceWa.Log.Errorf("Failed to get community participants: %v", err)
		} else {
			InstanceWa.Log.Infof("Community participants: %+v", resp)
		}
	case "listgroups":
		groups, err := InstanceWa.Client.GetJoinedGroups()
		if err != nil {
			InstanceWa.Log.Errorf("Failed to get group list: %v", err)
		} else {
			for _, group := range groups {
				InstanceWa.Log.Infof("%+v", group)
			}
		}
	case "getinvitelink":
		if len(args) < 1 {
			InstanceWa.Log.Errorf("Usage: getinvitelink <jid> [--reset]")
			return
		}
		group, ok := ParseJID(args[0])
		if !ok {
			return
		} else if group.Server != types.GroupServer {
			InstanceWa.Log.Errorf("Input must be a group JID (@%s)", types.GroupServer)
			return
		}
		resp, err := InstanceWa.Client.GetGroupInviteLink(group, len(args) > 1 && args[1] == "--reset")
		if err != nil {
			InstanceWa.Log.Errorf("Failed to get group invite link: %v", err)
		} else {
			InstanceWa.Log.Infof("Group invite link: %s", resp)
		}
	case "queryinvitelink":
		if len(args) < 1 {
			InstanceWa.Log.Errorf("Usage: queryinvitelink <link>")
			return
		}
		resp, err := InstanceWa.Client.GetGroupInfoFromLink(args[0])
		if err != nil {
			InstanceWa.Log.Errorf("Failed to resolve group invite link: %v", err)
		} else {
			InstanceWa.Log.Infof("Group info: %+v", resp)
		}
	case "querybusinesslink":
		if len(args) < 1 {
			InstanceWa.Log.Errorf("Usage: querybusinesslink <link>")
			return
		}
		resp, err := InstanceWa.Client.ResolveBusinessMessageLink(args[0])
		if err != nil {
			InstanceWa.Log.Errorf("Failed to resolve business message link: %v", err)
		} else {
			InstanceWa.Log.Infof("Business info: %+v", resp)
		}
	case "joininvitelink":
		if len(args) < 1 {
			InstanceWa.Log.Errorf("Usage: acceptinvitelink <link>")
			return
		}
		groupID, err := InstanceWa.Client.JoinGroupWithLink(args[0])
		if err != nil {
			InstanceWa.Log.Errorf("Failed to join group via invite link: %v", err)
		} else {
			InstanceWa.Log.Infof("Joined %s", groupID)
		}
	case "getstatusprivacy":
		resp, err := InstanceWa.Client.GetStatusPrivacy()
		fmt.Println(err)
		fmt.Println(resp)
	case "setdisappeartimer":
		if len(args) < 2 {
			InstanceWa.Log.Errorf("Usage: setdisappeartimer <jid> <days>")
			return
		}
		days, err := strconv.Atoi(args[1])
		if err != nil {
			InstanceWa.Log.Errorf("Invalid duration: %v", err)
			return
		}
		recipient, ok := ParseJID(args[0])
		if !ok {
			return
		}
		err = InstanceWa.Client.SetDisappearingTimer(recipient, time.Duration(days)*24*time.Hour)
		if err != nil {
			InstanceWa.Log.Errorf("Failed to set disappearing timer: %v", err)
		}
	case "send":
		if len(args) < 2 {
			InstanceWa.Log.Errorf("Usage: send <jid> <text>")
			return
		}
		recipient, ok := ParseJID(args[0])
		if !ok {
			return
		}
		msg := &waProto.Message{Conversation: proto.String(strings.Join(args[1:], " "))}
		resp, err := InstanceWa.Client.SendMessage(context.Background(), recipient, msg)
		if err != nil {
			InstanceWa.Log.Errorf("Error sending message: %v", err)
		} else {
			InstanceWa.Log.Infof("Message sent (server timestamp: %s)", resp.Timestamp)
		}
	case "sendpoll":
		if len(args) < 7 {
			InstanceWa.Log.Errorf("Usage: sendpoll <jid> <max answers> <question> -- <option 1> / <option 2> / ...")
			return
		}
		recipient, ok := ParseJID(args[0])
		if !ok {
			return
		}
		maxAnswers, err := strconv.Atoi(args[1])
		if err != nil {
			InstanceWa.Log.Errorf("Number of max answers must be an integer")
			return
		}
		remainingArgs := strings.Join(args[2:], " ")
		question, optionsStr, _ := strings.Cut(remainingArgs, "--")
		question = strings.TrimSpace(question)
		options := strings.Split(optionsStr, "/")
		for i, opt := range options {
			options[i] = strings.TrimSpace(opt)
		}
		resp, err := InstanceWa.Client.SendMessage(context.Background(), recipient, InstanceWa.Client.BuildPollCreation(question, options, maxAnswers))
		if err != nil {
			InstanceWa.Log.Errorf("Error sending message: %v", err)
		} else {
			InstanceWa.Log.Infof("Message sent (server timestamp: %s)", resp.Timestamp)
		}
	case "multisend":
		if len(args) < 3 {
			InstanceWa.Log.Errorf("Usage: multisend <jids...> -- <text>")
			return
		}
		var recipients []types.JID
		for len(args) > 0 && args[0] != "--" {
			recipient, ok := ParseJID(args[0])
			args = args[1:]
			if !ok {
				return
			}
			recipients = append(recipients, recipient)
		}
		if len(args) == 0 {
			InstanceWa.Log.Errorf("Usage: multisend <jids...> -- <text> (the -- is required)")
			return
		}
		msg := &waProto.Message{Conversation: proto.String(strings.Join(args[1:], " "))}
		for _, recipient := range recipients {
			go func(recipient types.JID) {
				resp, err := InstanceWa.Client.SendMessage(context.Background(), recipient, msg)
				if err != nil {
					InstanceWa.Log.Errorf("Error sending message to %s: %v", recipient, err)
				} else {
					InstanceWa.Log.Infof("Message sent to %s (server timestamp: %s)", recipient, resp.Timestamp)
				}
			}(recipient)
		}
	case "react":
		if len(args) < 3 {
			InstanceWa.Log.Errorf("Usage: react <jid> <message ID> <reaction>")
			return
		}
		recipient, ok := ParseJID(args[0])
		if !ok {
			return
		}
		messageID := args[1]
		fromMe := false
		if strings.HasPrefix(messageID, "me:") {
			fromMe = true
			messageID = messageID[len("me:"):]
		}
		reaction := args[2]
		if reaction == "remove" {
			reaction = ""
		}
		msg := &waProto.Message{
			ReactionMessage: &waProto.ReactionMessage{
				Key: &waProto.MessageKey{
					RemoteJid: proto.String(recipient.String()),
					FromMe:    proto.Bool(fromMe),
					Id:        proto.String(messageID),
				},
				Text:              proto.String(reaction),
				SenderTimestampMs: proto.Int64(time.Now().UnixMilli()),
			},
		}
		resp, err := InstanceWa.Client.SendMessage(context.Background(), recipient, msg)
		if err != nil {
			InstanceWa.Log.Errorf("Error sending reaction: %v", err)
		} else {
			InstanceWa.Log.Infof("Reaction sent (server timestamp: %s)", resp.Timestamp)
		}
	case "revoke":
		if len(args) < 2 {
			InstanceWa.Log.Errorf("Usage: revoke <jid> <message ID>")
			return
		}
		recipient, ok := ParseJID(args[0])
		if !ok {
			return
		}
		messageID := args[1]
		resp, err := InstanceWa.Client.SendMessage(context.Background(), recipient, InstanceWa.Client.BuildRevoke(recipient, types.EmptyJID, messageID))
		if err != nil {
			InstanceWa.Log.Errorf("Error sending revocation: %v", err)
		} else {
			InstanceWa.Log.Infof("Revocation sent (server timestamp: %s)", resp.Timestamp)
		}
	case "sendimg":
		if len(args) < 2 {
			InstanceWa.Log.Errorf("Usage: sendimg <jid> <image path> [caption]")
			return
		}
		recipient, ok := ParseJID(args[0])
		if !ok {
			return
		}
		data, err := os.ReadFile(args[1])
		if err != nil {
			InstanceWa.Log.Errorf("Failed to read %s: %v", args[0], err)
			return
		}
		uploaded, err := InstanceWa.Client.Upload(context.Background(), data, whatsmeow.MediaImage)
		if err != nil {
			InstanceWa.Log.Errorf("Failed to upload file: %v", err)
			return
		}
		msg := &waProto.Message{ImageMessage: &waProto.ImageMessage{
			Caption:       proto.String(strings.Join(args[2:], " ")),
			Url:           proto.String(uploaded.URL),
			DirectPath:    proto.String(uploaded.DirectPath),
			MediaKey:      uploaded.MediaKey,
			Mimetype:      proto.String(http.DetectContentType(data)),
			FileEncSha256: uploaded.FileEncSHA256,
			FileSha256:    uploaded.FileSHA256,
			FileLength:    proto.Uint64(uint64(len(data))),
		}}
		resp, err := InstanceWa.Client.SendMessage(context.Background(), recipient, msg)
		if err != nil {
			InstanceWa.Log.Errorf("Error sending image message: %v", err)
		} else {
			InstanceWa.Log.Infof("Image message sent (server timestamp: %s)", resp.Timestamp)
		}
	case "setstatus":
		if len(args) == 0 {
			InstanceWa.Log.Errorf("Usage: setstatus <message>")
			return
		}
		err := InstanceWa.Client.SetStatusMessage(strings.Join(args, " "))
		if err != nil {
			InstanceWa.Log.Errorf("Error setting status message: %v", err)
		} else {
			InstanceWa.Log.Infof("Status updated")
		}
	case "archive":
		if len(args) < 2 {
			InstanceWa.Log.Errorf("Usage: archive <jid> <action>")
			return
		}
		target, ok := ParseJID(args[0])
		if !ok {
			return
		}
		action, err := strconv.ParseBool(args[1])
		if err != nil {
			InstanceWa.Log.Errorf("invalid second argument: %v", err)
			return
		}

		err = InstanceWa.Client.SendAppState(appstate.BuildArchive(target, action, time.Time{}, nil))
		if err != nil {
			InstanceWa.Log.Errorf("Error changing chat's archive state: %v", err)
		}
	case "mute":
		if len(args) < 2 {
			InstanceWa.Log.Errorf("Usage: mute <jid> <action>")
			return
		}
		target, ok := ParseJID(args[0])
		if !ok {
			return
		}
		action, err := strconv.ParseBool(args[1])
		if err != nil {
			InstanceWa.Log.Errorf("invalid second argument: %v", err)
			return
		}

		err = InstanceWa.Client.SendAppState(appstate.BuildMute(target, action, 1*time.Hour))
		if err != nil {
			InstanceWa.Log.Errorf("Error changing chat's mute state: %v", err)
		}
	case "pin":
		if len(args) < 2 {
			InstanceWa.Log.Errorf("Usage: pin <jid> <action>")
			return
		}
		target, ok := ParseJID(args[0])
		if !ok {
			return
		}
		action, err := strconv.ParseBool(args[1])
		if err != nil {
			InstanceWa.Log.Errorf("invalid second argument: %v", err)
			return
		}

		err = InstanceWa.Client.SendAppState(appstate.BuildPin(target, action))
		if err != nil {
			InstanceWa.Log.Errorf("Error changing chat's pin state: %v", err)
		}
	}
}

// Метод обрабатывает callback
func handler(rawEvt interface{}) {
	switch evt := rawEvt.(type) {
	case *events.AppStateSyncComplete:
		if len(InstanceWa.Client.Store.PushName) > 0 && evt.Name == appstate.WAPatchCriticalBlock {
			err := InstanceWa.Client.SendPresence(types.PresenceAvailable)
			if err != nil {
				InstanceWa.Log.Warnf("Failed to send available presence: %v", err)
			} else {
				InstanceWa.Log.Infof("Marked self as available")
			}
		}
	case *events.Connected, *events.PushNameSetting:
		if len(InstanceWa.Client.Store.PushName) == 0 {
			return
		}
		// Send presence available when connecting and when the pushname is changed.
		// This makes sure that outgoing messages always have the right pushname.
		err := InstanceWa.Client.SendPresence(types.PresenceAvailable)
		if err != nil {
			InstanceWa.Log.Warnf("Failed to send available presence: %v", err)
		} else {
			InstanceWa.Log.Infof("Marked self as available")
		}
	case *events.StreamReplaced:
		os.Exit(0)
	case *events.Message:
		metaParts := []string{fmt.Sprintf("pushname: %s", evt.Info.PushName), fmt.Sprintf("timestamp: %s", evt.Info.Timestamp)}
		if evt.Info.Type != "" {
			metaParts = append(metaParts, fmt.Sprintf("type: %s", evt.Info.Type))
		}
		if evt.Info.Category != "" {
			metaParts = append(metaParts, fmt.Sprintf("category: %s", evt.Info.Category))
		}
		if evt.IsViewOnce {
			metaParts = append(metaParts, "view once")
		}
		if evt.IsViewOnce {
			metaParts = append(metaParts, "ephemeral")
		}
		if evt.IsViewOnceV2 {
			metaParts = append(metaParts, "ephemeral (v2)")
		}
		if evt.IsDocumentWithCaption {
			metaParts = append(metaParts, "document with caption")
		}
		if evt.IsEdit {
			metaParts = append(metaParts, "edit")
		}

		InstanceWa.Log.Infof("Received message %s from %s (%s): %+v", evt.Info.ID, evt.Info.SourceString(), strings.Join(metaParts, ", "), evt.Message)

		// если текстовое сообщение
		if evt.Info.Type == "text" && evt.Info.Category != "peer" {

			// сериализуем сообщение
			jsonData, err := json.Marshal(evt)

			// создаем объект сообщения
			dataMessage := properties.DataMessage{
				ChatId:           evt.Info.Chat.String(),
				MessageId:        evt.Info.ID,
				MessageTimestamp: uint64(evt.Info.Timestamp.Unix()),
				JsonData:         string(jsonData),
				MessageStatus:    3,
				StatusTimestamp:  uint64(evt.Info.Timestamp.Unix()),
			}

			// сохраняем сообщение в историю
			err = InstanceWa.Client.HistorySync([]properties.DataMessage{dataMessage})

			// если ошибка
			if err != nil {

				// выводим ошибку
				fmt.Errorf("error HistorySync %v", err)
			}

			// создаем объект данных webhook о новом сообщении
			newMessageWebhook := webhook.NewMessageWebhook{
				TypeWebhook:     "newMessage",
				WebhookUrl:      InstanceWa.WebhookUrl,
				CountTrySending: 0,
				InstanceWhatsapp: webhook.InstanceWhatsappWebhook{
					IdInstance: 0,
					Wid:        InstanceWa.Client.Store.ID.User + "@c.us",
				},
				Timestamp: time.Now().Unix(),
				NewMessage: webhook.NewMessage{
					Chat: webhook.ChatDataWhatsappMessage{
						ChatId:    evt.Info.Chat.User + "@c.us",
						FromMe:    evt.Info.IsFromMe,
						IdMessage: evt.Info.ID,
					},
					Message: webhook.DataWhatsappMessage{
						TypeMessage: "textMessage",
						Text:        *evt.Message.Conversation,
					},
					MessageTimestamp: evt.Info.Timestamp.Unix(),
					Status:           "delivered",
				},
			}

			// отправляем вебхук
			newMessageWebhook.SendNewMessageWebhook(InstanceWa.Log)
		}

		if evt.Message.GetPollUpdateMessage() != nil {
			decrypted, err := InstanceWa.Client.DecryptPollVote(evt)
			if err != nil {
				InstanceWa.Log.Errorf("Failed to decrypt vote: %v", err)
			} else {
				InstanceWa.Log.Infof("Selected options in decrypted vote:")
				for _, option := range decrypted.SelectedOptions {
					InstanceWa.Log.Infof("- %X", option)
				}
			}
		} else if evt.Message.GetEncReactionMessage() != nil {
			decrypted, err := InstanceWa.Client.DecryptReaction(evt)
			if err != nil {
				InstanceWa.Log.Errorf("Failed to decrypt encrypted reaction: %v", err)
			} else {
				InstanceWa.Log.Infof("Decrypted reaction: %+v", decrypted)
			}
		}

		img := evt.Message.GetImageMessage()
		if img != nil {
			data, err := InstanceWa.Client.Download(img)
			if err != nil {
				InstanceWa.Log.Errorf("Failed to download image: %v", err)
				return
			}
			exts, _ := mime.ExtensionsByType(img.GetMimetype())
			path := fmt.Sprintf("%s%s", evt.Info.ID, exts[0])
			err = os.WriteFile(path, data, 0600)
			if err != nil {
				InstanceWa.Log.Errorf("Failed to save image: %v", err)
				return
			}
			InstanceWa.Log.Infof("Saved image in message to %s", path)
		}
	case *events.Receipt:

		if evt.Type == events.ReceiptTypeRead || evt.Type == events.ReceiptTypeReadSelf {

			// выводим лог
			InstanceWa.Log.Infof("%v was read by %s at %s", evt.MessageIDs, evt.SourceString(), evt.Timestamp)

			// обходим массив идентифкаторов сообщений
			for _, idMessage := range evt.MessageIDs {

				// создаем объект сообщения
				dataMessage := properties.DataMessage{
					MessageId:       idMessage,
					MessageStatus:   4,
					StatusTimestamp: uint64(evt.Timestamp.Unix()),
				}

				// сохраняем сообщение в историю
				err := InstanceWa.Client.UpdateStatusMessage(dataMessage)

				// если ошибка
				if err != nil {

					// выводим ошибку
					fmt.Errorf("error UpdateStatusMessage %v", err)
				}

				//создаем структуру вебхук о статусе сообщения
				statusMessageWebhook := webhook.StatusMessageWebhook{
					TypeWebhook:     "statusMessage",
					WebhookUrl:      InstanceWa.WebhookUrl,
					CountTrySending: 0,
					InstanceWhatsapp: webhook.InstanceWhatsappWebhook{
						IdInstance: 0,
						Wid:        InstanceWa.Client.Store.ID.User + "@c.us",
					},
					Timestamp: time.Now().Unix(),
					StatusMessage: webhook.DataStatusMessage{
						IdMessage:       idMessage,
						TimestampStatus: evt.Timestamp.Unix(),
						Status:          "read",
					},
				}

				//отправляем вебхук
				statusMessageWebhook.SendStatusMessageWebhook(InstanceWa.Log)
			}

		} else if evt.Type == events.ReceiptTypeDelivered {

			//выводим лог
			InstanceWa.Log.Infof("%s was delivered to %s at %s", evt.MessageIDs[0], evt.SourceString(), evt.Timestamp)

			// обходим массив идентифкаторов сообщений
			for _, idMessage := range evt.MessageIDs {

				// создаем объект сообщения
				dataMessage := properties.DataMessage{
					MessageId:       idMessage,
					MessageStatus:   3,
					StatusTimestamp: uint64(evt.Timestamp.Unix()),
				}

				// сохраняем сообщение в историю
				err := InstanceWa.Client.UpdateStatusMessage(dataMessage)

				// если ошибка
				if err != nil {

					// выводим ошибку
					fmt.Errorf("error UpdateStatusMessage %v", err)
				}

				//создаем структуру вебхук о статусе сообщения
				statusMessageWebhook := webhook.StatusMessageWebhook{
					TypeWebhook:     "statusMessage",
					WebhookUrl:      InstanceWa.WebhookUrl,
					CountTrySending: 0,
					InstanceWhatsapp: webhook.InstanceWhatsappWebhook{
						IdInstance: 0,
						Wid:        InstanceWa.Client.Store.ID.User + "@c.us",
					},
					Timestamp: time.Now().Unix(),
					StatusMessage: webhook.DataStatusMessage{
						IdMessage:       idMessage,
						TimestampStatus: evt.Timestamp.Unix(),
						Status:          "delivered",
					},
				}

				//отправляем вебхук
				statusMessageWebhook.SendStatusMessageWebhook(InstanceWa.Log)
			}
		}
	case *events.Presence:

		// если канал не инициализирован
		if InstanceWa.ChainResponseGetStatusAccount != nil {

			// инициализируем объект ответа на получение информации о пользователе
			responseGetStatusAccount := properties.ResponseGetStatusAccount{}

			// если не доступен
			if evt.Unavailable {

				// если время последнего посещения ноль
				if evt.LastSeen.IsZero() {

					// выводим лог
					InstanceWa.Log.Infof("%s is now offline", evt.From)

					// пишем что пользователь offline
					responseGetStatusAccount.StatusAvailable = "offline"

					// передаем объект ответа на получение информации о пользователе в канал
					InstanceWa.ChainResponseGetStatusAccount <- responseGetStatusAccount

				} else {

					// выводим лог
					InstanceWa.Log.Infof("%s is now offline (last seen: %s)", evt.From, evt.LastSeen)

					// пишем что пользователь offline
					responseGetStatusAccount.StatusAvailable = "offline"

					// пишем время последнего визита
					responseGetStatusAccount.LastVisit = evt.UnixLastSeen

					// передаем объект ответа на получение информации о пользователе в канал
					InstanceWa.ChainResponseGetStatusAccount <- responseGetStatusAccount
				}
			} else {

				// выводим лог
				InstanceWa.Log.Infof("%s is now online", evt.From)

				// пишем что пользователь online
				responseGetStatusAccount.StatusAvailable = "online"

				// передаем объект ответа на получение информации о пользователе в канал
				InstanceWa.ChainResponseGetStatusAccount <- responseGetStatusAccount
			}
		}
	case *events.HistorySync:

		var messages []properties.DataMessage

		// если есть данные сообщений
		if evt.Data.Conversations != nil {

			// обходим данные сообщений
			for _, conversation := range evt.Data.Conversations {

				// если есть данные сообщений
				if conversation != nil {

					//обходим сообщения
					for _, message := range conversation.Messages {

						jsonData, err := json.Marshal(message)

						if err != nil {

							fmt.Errorf("error marshal message %v", err)
						} else {

							dataMessage := properties.DataMessage{
								ChatId:           *conversation.Id,
								MessageId:        *message.Message.Key.Id,
								MessageTimestamp: *message.Message.MessageTimestamp,
								JsonData:         string(jsonData),
								MessageStatus:    0,
								StatusTimestamp:  *message.Message.MessageTimestamp,
							}

							if message.Message.Status != nil {
								dataMessage.MessageStatus = int32(*message.Message.Status)
							}

							messages = append(messages, dataMessage)
						}
					}
				}
			}
		}

		err := InstanceWa.Client.HistorySync(messages)

		if err != nil {

			fmt.Errorf("error saveOrUpdateMessage %v", err)
		}
	case *events.AppState:
		InstanceWa.Log.Debugf("App state event: %+v / %+v", evt.Index, evt.SyncActionValue)
	case *events.KeepAliveTimeout:
		InstanceWa.Log.Debugf("Keepalive timeout event: %+v", evt)
	case *events.KeepAliveRestored:
		InstanceWa.Log.Debugf("Keepalive restored")
	}
}
