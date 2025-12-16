package smsmtsds

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"sync"
	"sync/atomic"
	"time"
)

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
	SMSProvider        string `xml:"smsprovider" json:"smsprovider"`
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

func checkMStateInQMTS(DataInQ *stfordata) (logmsg string, FinalStatusCode bool) {
	EndStatuscode := false
	LogStatusText := ""
	switch DataInQ.statuscode {
	case STATUSCODE_Sending:
		//единственный случай когда мы будем пробовать получить статус позже
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
		EndStatuscode = true
		break
	default:
		LogStatusText = "status not available"
		EndStatuscode = true
	}
	return LogStatusText, EndStatuscode
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
	client := &http.Client{Transport: transport, Timeout: time.Duration(APIstruct.HttTimeoutSeconds) * time.Second}

	var MTSj MTSJson
	var MTSMsgEone MTSmessage
	var MTSopt MTSOptions
	MsgId := atomic.AddUint64(&APIstruct.CMsgIdInt, 1)
	MsgIds := fmt.Sprintf("MSGZID-%d", MsgId)
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
	nreq, nerr := http.NewRequest("POST", APIstruct.sendurl, bytes.NewBuffer(Bt))
	if nerr != nil {
		return nerr
	}
	nreq.SetBasicAuth(APIstruct.Username, APIstruct.Password)
	nreq.Header.Set("Content-Type", "application/json; charset=UTF-8")
	nresp, nresperr := client.Do(nreq)
	if nresperr != nil {
		return nresperr
	}
	defer nresp.Body.Close()
	repbody, ioerr := io.ReadAll(nresp.Body)
	if ioerr != nil {
		return ioerr
	}

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
		(*APIstruct.msg_intid_q_status) = append(*APIstruct.msg_intid_q_status, stfordata{time.Now().Unix(), MsgIds, InternalId, 0, 0, 0, "", ClienName, PhoneNumber, 0})
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

	var KeysForWireshark io.Writer
	KeysForWireshark = nil
	if APIstruct.Debuggmode > 250 {
		kf, fileerr := os.OpenFile("keys", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
		if fileerr != nil {
			return RespRecords, fileerr
		}
		defer kf.Close()
		KeysForWireshark = kf
	}

	tlsConfig := &tls.Config{
		KeyLogWriter: KeysForWireshark,
	}

	transport := &http.Transport{TLSClientConfig: tlsConfig}
	client := &http.Client{Transport: transport, Timeout: time.Duration(APIstruct.HttTimeoutSeconds) * time.Second}

	MTSStat.IntIds = append(MTSStat.IntIds, IntId)
	JsonBody, BodyMarshalErr := json.Marshal(MTSStat)
	if BodyMarshalErr != nil {
		return RespRecords, BodyMarshalErr
	}
	sreq, serr := http.NewRequest("POST", APIstruct.statusurl, bytes.NewBuffer(JsonBody))
	if serr != nil {
		return RespRecords, serr
	}
	sreq.SetBasicAuth(APIstruct.Username, APIstruct.Password)
	sreq.Header.Set("Content-Type", "application/json; charset=UTF-8")
	sresp, sresperr := client.Do(sreq)
	if sresperr != nil {
		return RespRecords, sresperr
	}
	defer sresp.Body.Close()
	sepbody, ioerr := io.ReadAll(sresp.Body)
	if ioerr != nil {
		return RespRecords, ioerr
	}

	RespUnErr := json.Unmarshal(sepbody, &RespRecords)
	if RespUnErr != nil {
		return RespRecords, RespUnErr
	}
	return RespRecords, nil
}

func (APIstruct *SMSapi) mtsQStatus(ctx context.Context, wg *sync.WaitGroup) {
	defer wg.Done()
	if !APIstruct.initialized {
		return
	}
	TimerCheckusers := time.NewTicker(1 * time.Second)

	qstatcounter := 0
	for {
		select {
		case <-ctx.Done():
			TimerCheckusers.Stop()
			return

		case <-TimerCheckusers.C:
			APIstruct.msg_qmutex.Lock()
			if len(*APIstruct.msg_intid_q_status) > 0 {
				EndStatuscode := false
				Msg := (*APIstruct.msg_intid_q_status)[0]
				//Если прошло более 3 секунд с момента отправки сообщения, то получим статус отправки
				if time.Now().Unix()-Msg.senttime >= 3 {
					//Если прошла минута, а статус получить не можем то, более не будем пытаться
					//Если прошло более 3 секунд с момента отправки сообщения, то получим статус отправки
					if time.Now().Unix()-Msg.senttime >= 60 {
						LogStatusText := "status not changed until 60 second"
						LogMsg := fmt.Sprintf("Client: %s, message local id: %s, remote id: %s to: %s ,status: %s", Msg.senderip, Msg.msgid, Msg.intmsgid, Msg.destination, LogStatusText)
						APIstruct.loggerp.Println(LogMsg)
						EndStatuscode = true
					} else {
						//Получаем статусы
						APIstruct.msg_qmutex.Unlock()
						RespRecords, RsErr := APIstruct.statusMessageMTS(Msg.intmsgid)
						APIstruct.msg_qmutex.Lock()
						//Повторно проверяем если в очереди на запрос статуса есть хотябы одно сообщение
						if len(*APIstruct.msg_intid_q_status) > 0 {
							if RsErr == nil {
								CStatusCode := 0
								for _, StatusEntry := range RespRecords.Events_infoAr {
									for _, CEvents_info_int := range StatusEntry.Events_info_int {
										var LogStatusText string
										CStatusCode = CEvents_info_int.Status
										if CStatusCode != Msg.statuscode {
											(*APIstruct.msg_intid_q_status)[0].statuscode = CStatusCode
											LogStatusText, EndStatuscode = checkMStateInQMTS(&(*APIstruct.msg_intid_q_status)[0])
											/*switch CStatusCode {
											case STATUSCODE_Sending:
												//единственный случай когда мы будем пробовать получить статус позже
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
												EndStatuscode = true
												break
											default:
												LogStatusText = "status not avalible"
												EndStatuscode = true
											}

											*/
											LogMsg := fmt.Sprintf("Client: %s, message local id: %s, remote id: %s to: %s ,status: %s", Msg.senderip, Msg.msgid, Msg.intmsgid, CEvents_info_int.Destination, LogStatusText)
											APIstruct.loggerp.Println(LogMsg)
										}
									}
								}
							} else {
								(*APIstruct.msg_intid_q_status)[0].getdataretr++
								if (*APIstruct.msg_intid_q_status)[0].getdataretr >= 3 || time.Now().Unix()-Msg.senttime >= 30 {
									EndStatuscode = true
								}
								LogMsg := fmt.Sprintf("API error: %s", RsErr)
								APIstruct.loggerp.Println(LogMsg)
							}
						}
					}
				}
				if EndStatuscode {
					(*APIstruct.msg_intid_q_status) = (*APIstruct.msg_intid_q_status)[1:]
				}
			}
			qstatcounter++
			if qstatcounter >= 60 {
				qstatcounter = 0
				LogMsg := fmt.Sprintf("Status queue len: %d", len((*APIstruct.msg_intid_q_status)))
				APIstruct.loggerp.Println(LogMsg)
			}
			APIstruct.msg_qmutex.Unlock()
		}
	}
}
