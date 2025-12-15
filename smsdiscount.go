package mtssmsdiscount

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

const DS_STATUSSTRING_Sending_na = "not avalible"
const DS_STATUSSTRING_Sending_Accepted = "accepted"
const DS_STATUSSTRING_Sending_invalid = "sender address invalid"
const DS_STATUSSTRING_Sending_rejected = "smsc reject"
const DS_STATUSSTRING_Sending_delivered = "delivered"

type SMSDiscontMessages struct {
	Login    string       `json:"login"`
	Password string       `json:"password"`
	Messages []SMSDiscont `json:"messages"`
}

type SMSDiscontReqMessagesStatus struct {
	Login    string                       `json:"login"`
	Password string                       `json:"password"`
	Messages []SMSDiscontReqMessageStatus `json:"messages"`
}

type SMSDiscontReqMessageStatus struct {
	ClientID string `json:"clientId"`
	SMSID    string `json:"smscId"`
}

type SMSDiscontMessagesStatus struct {
	ClientID uint64 `json:"clientId"`
	SMSID    uint64 `json:"smscId"`
	Status   string `json:"status"`
}

type SMSDiscont struct {
	ClientID uint64 `json:"clientid"`
	Phone    string `json:"phone"`
	Text     string `json:"text"`
}

type SMSDiscontStatus struct {
	Status      string                     `json:"status"`
	Description string                     `json:"description"`
	Messages    []SMSDiscontMessagesStatus `json:"messages"`
}

type SMSDiscontMessagesGetStatus struct {
	ClientID uint64       `json:"clientid"`
	Phone    string       `json:"phone"`
	Messages []SMSDiscont `json:"messages"`
}

func (APIstruct *SMSapi) sendMessageDS(ClienName string, PhoneNumber string, Data string) error {
	if !APIstruct.initialized {
		return fmt.Errorf("API Struct not inicialized")
	}
	NormalizedNumber, NnErr := APIstruct.NormalizePhoneNumber(PhoneNumber)
	if NnErr != nil {
		return NnErr
	}

	//InsecureTF := true
	var KeysForWireshark io.Writer
	KeysForWireshark = nil
	if APIstruct.Debuggmode > 250 {
		kf, fileerr := os.OpenFile("keys", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
		if fileerr != nil {
			return fileerr
		}
		defer kf.Close()
		KeysForWireshark = kf
	}

	tlsConfig := &tls.Config{
		//InsecureSkipVerify: InsecureTF,
		KeyLogWriter: KeysForWireshark,
	}

	transport := &http.Transport{TLSClientConfig: tlsConfig}
	client := &http.Client{Transport: transport}

	var JsData SMSDiscont
	var JsonAll SMSDiscontMessages
	APIstruct.CMsgIdInt += 1
	MsgIds := APIstruct.CMsgIdInt
	JsData.Phone = NormalizedNumber
	JsonAll.Login = APIstruct.Username
	JsonAll.Password = APIstruct.Password
	JsData.ClientID = MsgIds
	JsData.Text = Data
	JsonAll.Messages = append(JsonAll.Messages, JsData)

	Bt, Mer := json.Marshal(JsonAll)
	if Mer != nil {
		return Mer
	}
	//fmt.Println(string(Bt), Mer)
	nreq, nerr := http.NewRequest("POST", APIstruct.Sendurl, bytes.NewBuffer(Bt))
	if nerr != nil {
		return nerr
	}

	nreq.Header.Set("Content-Type", "application/json; charset=UTF-8")
	nresp, nresperr := client.Do(nreq)
	if nresperr != nil {
		return nresperr
	}
	repbody, _ := ioutil.ReadAll(nresp.Body)
	var RespBodyData SMSDiscontStatus
	ummerr := json.Unmarshal(repbody, &RespBodyData)
	if ummerr != nil {
		return ummerr
	}
	if len(RespBodyData.Messages) == 0 {
		return fmt.Errorf("error: no data")
	}
	if strings.ToLower(RespBodyData.Status) != "ok" {
		return fmt.Errorf("status: %s, description: %s", RespBodyData.Status, RespBodyData.Description)
	}

	if len(RespBodyData.Messages) == 1 {
		if strings.ToLower(RespBodyData.Messages[0].Status) != "accepted" {
			return fmt.Errorf("status: %s, messagestatus: %s", RespBodyData.Status, RespBodyData.Messages[0].Status)
		}
	}
	InternalId := RespBodyData.Messages[0].SMSID
	if InternalId > 0 {
		APIstruct.msg_qmutex.Lock()
		(*APIstruct.msg_intid_q_status) = append(*APIstruct.msg_intid_q_status, stfordata{time.Now().Unix(), "", "", MsgIds, InternalId, 0, DS_STATUSSTRING_Sending_na, ClienName, PhoneNumber})
		APIstruct.msg_qmutex.Unlock()
	}

	return nil
}

func (APIstruct *SMSapi) statusMessageDS(IntId string, RemId string) (Resp SMSDiscontStatus, StErr error) {
	var DSGetStatus SMSDiscontReqMessagesStatus
	var RespRecords SMSDiscontStatus
	DSGetStatus.Login = APIstruct.Username
	DSGetStatus.Password = APIstruct.Password
	if !APIstruct.initialized {
		return RespRecords, fmt.Errorf("APIstruct not initialized")
	}

	//InsecureTF := true
	tlsConfig := &tls.Config{
		//InsecureSkipVerify: InsecureTF,
	}

	transport := &http.Transport{TLSClientConfig: tlsConfig}
	client := &http.Client{Transport: transport}

	DSGetStatus.Messages = append(DSGetStatus.Messages, SMSDiscontReqMessageStatus{ClientID: IntId, SMSID: RemId})
	JsonBody, BodyMarshalErr := json.Marshal(DSGetStatus)
	if BodyMarshalErr != nil {
		return RespRecords, BodyMarshalErr
	}
	sreq, serr := http.NewRequest("POST", APIstruct.Stturl, bytes.NewBuffer(JsonBody))
	if serr != nil {
		return RespRecords, serr
	}
	sreq.Header.Set("Content-Type", "application/json; charset=UTF-8")
	sresp, sresperr := client.Do(sreq)
	if sresperr != nil {
		return RespRecords, sresperr
	}
	sepbody, _ := ioutil.ReadAll(sresp.Body)
	RespUnErr := json.Unmarshal(sepbody, &RespRecords)
	if RespUnErr == nil {
		return RespRecords, RespUnErr
	}

	return RespRecords, nil
}

func (APIstruct *SMSapi) DSQStatus(wg sync.WaitGroup) {
	defer wg.Done()
	if !APIstruct.initialized {
		return
	}
	qstatcounter := 0
	for {
		if len(*APIstruct.msg_intid_q_status) > 0 {
			EndStatuscode := false
			APIstruct.msg_qmutex.Lock()
			Msg := (*APIstruct.msg_intid_q_status)[len((*APIstruct.msg_intid_q_status))-1]
			if time.Now().Unix()-Msg.senttime >= 3 {
				RespRecords, RsErr := APIstruct.statusMessageDS(Msg.msgid, Msg.intmsgid)
				if RsErr == nil {
					PrStatus := RespRecords.Status
					if PrStatus != "ok" {
						EndStatuscode = true
						break
					}
					for _, StatusEntry := range RespRecords.Messages {
						LogStatusText := DS_STATUSSTRING_Sending_na
						CStatusCode := StatusEntry.Status
						if CStatusCode != Msg.stringstatuscode {
							(*APIstruct.msg_intid_q_status)[len((*APIstruct.msg_intid_q_status))-1].stringstatuscode = CStatusCode
							switch CStatusCode {
							case DS_STATUSSTRING_Sending_rejected:
								LogStatusText = "rejected"
								EndStatuscode = true
								break
							case DS_STATUSSTRING_Sending_delivered:
								LogStatusText = "delivered"
								EndStatuscode = true
								break
							default:
								LogStatusText = "status not avalible"
							}
							LogMsg := fmt.Sprintf("Client: %s, message to: %s ,startus: %s", Msg.senderip, Msg.destination, LogStatusText)
							APIstruct.loggerp.Println(LogMsg)
						}

					}
				}
			}
			if EndStatuscode {
				(*APIstruct.msg_intid_q_status) = (*APIstruct.msg_intid_q_status)[:len((*APIstruct.msg_intid_q_status))-1]
			}
			APIstruct.msg_qmutex.Unlock()
		}
		time.Sleep(time.Second * 1)
		qstatcounter++
		if qstatcounter >= 60 {
			qstatcounter = 0
			LogMsg := fmt.Sprintf("Status quaue len: %d", len((*APIstruct.msg_intid_q_status)))
			APIstruct.loggerp.Println(LogMsg)
		}
	}
}
