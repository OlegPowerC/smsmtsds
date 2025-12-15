package smsmtsds

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"sync"
	"time"
)

const STATUSURL = "https://omnichannel.mts.ru/http-api/v1/messages/info"
const SENDURL = "https://omnichannel.mts.ru/http-api/v1/messages"
const STATUSCODE_Sending = 200
const STATUSCODE_NotSent = 201
const STATUSCODE_PartSending = 202
const STATUSCODE_Delivered = 300
const STATUSCODE_NotDelivered = 301
const STATUSCODE_PartDelivered = 302

// Описание JSON параметров
type SMSSettings struct {
	SendUrl            string `xml:"sendurl" json:"sendurl"`
	StatusUrl          string `xml:"statusurl" json:"statusurl"`
	SMSLogin           string `xml:"smslogin" json:"smslogin"`
	SMSPassword        string `xml:"smspassword" json:"smspassword"`
	SMSName            string `xml:"smsname" json:"smsname"`
	Listenip           string `xml:"listenip" json:"listenip"`
	Listenport         int    `xml:"listenport" json:"listenport"`
	Numbertotallen     int    `json:"numbertotallen"`
	Defaultcountrycode int    `json:"defaultcountrycode"`
}

type MTSRespEventInfo struct {
	Call_direction string `json:"call_direction"`
	Channel        int    `json:"channel"`
	Client_id      int    `json:"client_id"`
	Destination    string `json:"destination"`
	Event_at       string `json:"event_at"`
	Internal_id    string `json:"internal_id"`
	Message_id     string `json:"message_id"`
	Naming         string `json:"naming"`
	Received_at    string `json:"received_at"`
	Status         int    `json:"status"`
	Total_parts    int    `json:"total_parts"`
}

type MTSRespEventsInfoInt struct {
	Events_info_int []MTSRespEventInfo `json:"events_info"`
}

type MTSResponse struct {
	Events_infoAr []MTSRespEventsInfoInt `json:"events_info"`
}

type MTSTo struct {
	Msisdn    string `json:"msisdn"`
	MessageId string `json:"message_id"`
}

type MTSGetStatus struct {
	//MsgIds []string `json:"msg_ids"`
	IntIds []string `json:"int_ids"`
}

type MTSOptions struct {
	Class int `json:"class"`
	From  struct {
		SmsAddress string `json:"sms_address"`
	} `json:"from"`
}

type MTSmessage struct {
	Content struct {
		ShortText string `json:"short_text"`
	} `json:"content"`
	To []MTSTo `json:"to"`
}

type MTSJson struct {
	Messages []MTSmessage `json:"messages"`
	Options  MTSOptions   `json:"options"`
}

type MTSMsRet struct {
	InternalId    string `json:"internal_id"`
	MessageId     string `json:"message_id"`
	MErrorcode    int    `json:"error_code"`
	MErrormessage string `json:"error_message"`
	Msisdn        string `json:"msisdn"`
}

type MTSRespFrom struct {
	Messages []MTSMsRet `json:"messages"`
}

func (APIstruct *SMSapi) Init() error {
	ErrStr := ""
	ErrCnt := 0
	if len(APIstruct.Username) == 0 {
		ErrStr += "please provide username "
		ErrCnt++
	}
	if len(APIstruct.Password) == 0 {
		ErrStr += "please provide password "
		ErrCnt++
	}
	if len(APIstruct.Logfile) == 0 {
		ErrStr += "please provide logfile name  "
		ErrCnt++
	}

	if ErrCnt > 0 {
		return fmt.Errorf("%s", ErrStr)
	}

	Qdata := make([]stfordata, 0)
	APIstruct.msg_intid_q_status = &Qdata

	//Открываем файл лога
	logFile, err := os.OpenFile(APIstruct.Logfile+"log.txt", os.O_RDWR|os.O_CREATE|os.O_APPEND, 0666)
	if err != nil {
		return err
	}

	APIstruct.loggerp = log.New(logFile, "", log.LstdFlags)
	APIstruct.loggerp.Println(fmt.Sprintf("INFO: %s", "Start logging"))

	APIstruct.initialized = true
	return nil
}

func (APIstruct *SMSapi) sendMessageMTS(ClienName string, PhoneNumber string, Data string) error {
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

	var MTSj MTSJson
	var MTSMsgEone MTSmessage
	var MTSopt MTSOptions
	APIstruct.CMsgIdInt += 1
	MsgIds := fmt.Sprintf("MSGZID-%d", APIstruct.CMsgIdInt)
	MTSMsgEone.Content.ShortText = Data
	MTSMsgEone.To = append(MTSMsgEone.To, MTSTo{Msisdn: NormalizedNumber, MessageId: MsgIds})
	MTSopt.From.SmsAddress = APIstruct.Naming
	MTSopt.Class = 1
	MTSj.Messages = append(MTSj.Messages, MTSMsgEone)
	MTSj.Options = MTSopt

	InternalId := ""

	Bt, Mer := json.Marshal(MTSj)
	if Mer != nil {
		return Mer
	}
	//fmt.Println(string(Bt), Mer)
	nreq, nerr := http.NewRequest("POST", SENDURL, bytes.NewBuffer(Bt))
	if nerr != nil {
		return nerr
	}
	nreq.SetBasicAuth(APIstruct.Username, APIstruct.Password)
	nreq.Header.Set("Content-Type", "application/json; charset=UTF-8")
	nresp, nresperr := client.Do(nreq)
	if nresperr != nil {
		return nresperr
	}
	repbody, _ := ioutil.ReadAll(nresp.Body)
	var RespBodyData MTSRespFrom
	ummerr := json.Unmarshal(repbody, &RespBodyData)
	if ummerr != nil {
		return ummerr
	}
	if len(RespBodyData.Messages) == 0 {
		return fmt.Errorf("error: no data")
	}
	if RespBodyData.Messages[0].MErrorcode > 0 {
		return fmt.Errorf("error: %d, %s", RespBodyData.Messages[0].MErrorcode, RespBodyData.Messages[0].MErrormessage)
	}
	InternalId = RespBodyData.Messages[0].InternalId
	if len(InternalId) > 0 {
		APIstruct.msg_qmutex.Lock()
		(*APIstruct.msg_intid_q_status) = append(*APIstruct.msg_intid_q_status, stfordata{time.Now().Unix(), MsgIds, InternalId, 0, 0, 0, "", ClienName, PhoneNumber})
		APIstruct.msg_qmutex.Unlock()
	}
	return nil
}

func (APIstruct *SMSapi) statusMessageMTS(IntId string) (Resp MTSResponse, StErr error) {
	var MTSStat MTSGetStatus
	var RespRecords MTSResponse
	if !APIstruct.initialized {
		return RespRecords, fmt.Errorf("APIstruct not initialized")
	}

	//InsecureTF := true
	tlsConfig := &tls.Config{
		//InsecureSkipVerify: InsecureTF,
	}

	transport := &http.Transport{TLSClientConfig: tlsConfig}
	client := &http.Client{Transport: transport}

	MTSStat.IntIds = append(MTSStat.IntIds, IntId)
	JsonBody, BodyMarshalErr := json.Marshal(MTSStat)
	if BodyMarshalErr != nil {
		return RespRecords, BodyMarshalErr
	}
	sreq, serr := http.NewRequest("POST", STATUSURL, bytes.NewBuffer(JsonBody))
	if serr != nil {
		return RespRecords, serr
	}
	sreq.SetBasicAuth(APIstruct.Username, APIstruct.Password)
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

func (APIstruct *SMSapi) QStatus(wg sync.WaitGroup) {
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
				RespRecords, RsErr := APIstruct.statusMessageMTS(Msg.intmsgid)
				if RsErr == nil {
					CStatusCode := 0
					for _, StatusEntry := range RespRecords.Events_infoAr {
						for _, CEvents_info_int := range StatusEntry.Events_info_int {
							LogStatusText := "status not avalible"
							CStatusCode = CEvents_info_int.Status
							if CStatusCode != Msg.statuscode {
								(*APIstruct.msg_intid_q_status)[len((*APIstruct.msg_intid_q_status))-1].statuscode = CStatusCode
								switch CStatusCode {
								case STATUSCODE_Sending:
									LogStatusText = "sending"
									break
								case STATUSCODE_NotSent:
									LogStatusText = "not sent"
									EndStatuscode = true
									break
								case STATUSCODE_PartSending:
									LogStatusText = "part sent"
									break
								case STATUSCODE_Delivered:
									LogStatusText = "delivered"
									EndStatuscode = true
									break
								case STATUSCODE_NotDelivered:
									LogStatusText = "not delivered"
									EndStatuscode = true
									break
								case STATUSCODE_PartDelivered:
									LogStatusText = "part delivered"
									break
								default:
									LogStatusText = "status not avalible"
								}
								LogMsg := fmt.Sprintf("Client: %s, message to: %s ,startus: %s", Msg.senderip, CEvents_info_int.Destination, LogStatusText)
								APIstruct.loggerp.Println(LogMsg)
							}
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
