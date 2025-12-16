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
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const DS_STATUSSTRING_Sending_na = "not available"
const DS_STATUSSTRING_Sending_queued = "queued"
const DS_STATUSSTRING_Sending_Accepted = "accepted"
const DS_STATUSSTRING_Sending_invalid = "sender address invalid"
const DS_STATUSSTRING_Sending_rejected = "smsc reject"
const DS_STATUSSTRING_Sending_delivered = "delivered"
const DS_STATUSSTRING_Sending_smssubmit = "sms submit"
const DS_STATUSSTRING_Sending_incorrect_id = "incorrect id"
const DS_STATUSSTRING_Sending_delivery_error = "delivery error"

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
	ClientID uint64 `json:"clientId"`
	SMSID    uint64 `json:"smscId"`
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
	client := &http.Client{Transport: transport, Timeout: time.Duration(APIstruct.HttTimeoutSeconds) * time.Second}

	var JsData SMSDiscont
	var JsonAll SMSDiscontMessages
	MsgIds := atomic.AddUint64(&APIstruct.CMsgIdInt, 1)
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
	nreq, nerr := http.NewRequest("POST", APIstruct.sendurl, bytes.NewBuffer(Bt))
	if nerr != nil {
		return nerr
	}

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

	if len(RespBodyData.Messages) > 0 {
		if strings.ToLower(RespBodyData.Messages[0].Status) != "accepted" {
			return fmt.Errorf("status: %s, messagestatus: %s", RespBodyData.Status, RespBodyData.Messages[0].Status)
		}
	}
	InternalId := RespBodyData.Messages[0].SMSID
	if InternalId > 0 {
		APIstruct.msg_qmutex.Lock()
		(*APIstruct.msg_intid_q_status) = append(*APIstruct.msg_intid_q_status, stfordata{time.Now().Unix(), "", "", MsgIds, InternalId, 0, DS_STATUSSTRING_Sending_na, ClienName, PhoneNumber, 0})
		APIstruct.msg_qmutex.Unlock()
	}

	return nil
}

func (APIstruct *SMSapi) statusMessageDS(IntId uint64, RemId uint64) (Resp SMSDiscontStatus, StErr error) {
	var DSGetStatus SMSDiscontReqMessagesStatus
	var RespRecords SMSDiscontStatus
	DSGetStatus.Login = APIstruct.Username
	DSGetStatus.Password = APIstruct.Password
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
	//InsecureTF := true
	tlsConfig := &tls.Config{
		//InsecureSkipVerify: InsecureTF,
		KeyLogWriter: KeysForWireshark,
	}

	transport := &http.Transport{TLSClientConfig: tlsConfig}
	client := &http.Client{Transport: transport, Timeout: time.Duration(APIstruct.HttTimeoutSeconds) * time.Second}

	DSGetStatus.Messages = append(DSGetStatus.Messages, SMSDiscontReqMessageStatus{ClientID: IntId, SMSID: RemId})
	JsonBody, BodyMarshalErr := json.Marshal(DSGetStatus)
	if BodyMarshalErr != nil {
		return RespRecords, BodyMarshalErr
	}
	sreq, serr := http.NewRequest("POST", APIstruct.statusurl, bytes.NewBuffer(JsonBody))
	if serr != nil {
		return RespRecords, serr
	}
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

func checkMStateInQDS(DSmsgstatus SMSDiscontMessagesStatus) (logmsg string, FinalStatusCode bool) {
	EndStatuscode := false
	LogStatusText := ""
	CStatusCode := DSmsgstatus.Status
	switch CStatusCode {
	case DS_STATUSSTRING_Sending_rejected:
		LogStatusText = "rejected"
		EndStatuscode = true
		break
	case DS_STATUSSTRING_Sending_delivered:
		LogStatusText = "delivered"
		EndStatuscode = true
		break
	case DS_STATUSSTRING_Sending_delivery_error:
		LogStatusText = "delivery error"
		EndStatuscode = true
		break
	case DS_STATUSSTRING_Sending_queued:
		LogStatusText = "queued"
		break
	case DS_STATUSSTRING_Sending_smssubmit:
		LogStatusText = "SMS submit"
		EndStatuscode = true
		break
	case DS_STATUSSTRING_Sending_invalid:
		LogStatusText = "sender address invalid"
		EndStatuscode = true
		break
	case DS_STATUSSTRING_Sending_incorrect_id:
		LogStatusText = "incorrect id"
		EndStatuscode = true
		break
	case DS_STATUSSTRING_Sending_Accepted:
		LogStatusText = "accepted"
		break
	default:
		LogStatusText = "status not available"
		EndStatuscode = true
	}
	return LogStatusText, EndStatuscode
}

func (APIstruct *SMSapi) dsQStatus(ctx context.Context, wg *sync.WaitGroup) {
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
				//Если в очереди на запрос статуса есть хотябы одно сообщение
				Msg := (*APIstruct.msg_intid_q_status)[0]
				//Если прошло более 3 секунд с момента отправки сообщения, то получим статус отправки
				if time.Now().Unix()-Msg.senttime >= 3 {
					//Если прошла минута, а статус получить не можем то, более не будем пытаться
					//Если прошло более 3 секунд с момента отправки сообщения, то получим статус отправки
					if time.Now().Unix()-Msg.senttime >= 60 {
						LogStatusText := "status not changed until 60 second"
						LogMsg := fmt.Sprintf("Client: %s, message local id: %s, remote id: %s to: %s ,status: %s", Msg.senderip, Msg.uintmsgid, Msg.uintintmsgid, Msg.destination, LogStatusText)
						APIstruct.loggerp.Println(LogMsg)
						EndStatuscode = true
					} else {
						//Получаем статусы
						APIstruct.msg_qmutex.Unlock()
						RespRecords, RsErr := APIstruct.statusMessageDS(Msg.uintmsgid, Msg.uintintmsgid)
						APIstruct.msg_qmutex.Lock()
						//Повторно проверяем если в очереди на запрос статуса есть хотябы одно сообщение
						if len(*APIstruct.msg_intid_q_status) > 0 {
							if RsErr == nil {
								PrStatus := RespRecords.Status
								if PrStatus != "ok" {
									EndStatuscode = true
								} else {
									for _, StatusEntry := range RespRecords.Messages {
										var LogStatusText string
										CStatusCode := StatusEntry.Status
										if CStatusCode != Msg.stringstatuscode {
											(*APIstruct.msg_intid_q_status)[0].stringstatuscode = CStatusCode
											LogStatusText, EndStatuscode = checkMStateInQDS(StatusEntry)
											if LogStatusText != DS_STATUSSTRING_Sending_na {
												LogMsg := fmt.Sprintf("Client: %s, message local id: %s, remote id: %s to: %s ,status: %s", Msg.senderip, Msg.msgid, Msg.uintintmsgid, Msg.destination, LogStatusText)
												APIstruct.loggerp.Println(LogMsg)
											}
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
	/*
		defer wg.Done()
		if !APIstruct.initialized {
			return
		}
		qstatcounter := 0
		for {
			APIstruct.msg_qmutex.Lock()
			if len(*APIstruct.msg_intid_q_status) > 0 {
				EndStatuscode := false
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
			}
			APIstruct.msg_qmutex.Unlock()
			time.Sleep(time.Second * 1)
			qstatcounter++
			if qstatcounter >= 60 {
				qstatcounter = 0
				LogMsg := fmt.Sprintf("Status quaue len: %d", len((*APIstruct.msg_intid_q_status)))
				APIstruct.loggerp.Println(LogMsg)
			}
		}
	*/
}
