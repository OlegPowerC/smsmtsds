package smsmtsds

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"
	"sync"
)

const MTSSTATUSURL = "https://omnichannel.mts.ru/http-api/v1/messages/info"
const MTSSENDURL = "https://omnichannel.mts.ru/http-api/v1/messages"
const DSSTATUSURL = "https://api.iqsms.ru/messages/v2/status.json"
const DSSSENDURL = "https://api.iqsms.ru/messages/v2/send.json"

const HTTP_DEFAULT_TIMEOUT_SEC = 10
const HTTP_MAX_TIMEOUT_SEC = 30

const SMS_PROVIDER_STRINGNAME_MTS = "mts"
const SMS_PROVIDER_STRINGNAME_DS = "sms discount"

type SMSapi struct {
	sendurl            string
	statusurl          string
	Username           string
	Naming             string
	Password           string
	Logfile            string
	msg_intid_q_status *[]stfordata
	msg_qmutex         sync.Mutex
	loggerp            *log.Logger
	CMsgIdInt          uint64
	initialized        bool
	Totalnumberlen     uint
	Defaultcountrycode uint
	Debuggmode         uint
	HttTimeoutSeconds  uint
	SMSProviderName    string
	smsprovicerIndex   uint
}

type stfordata struct {
	senttime         int64
	msgid            string
	intmsgid         string
	uintmsgid        uint64
	uintintmsgid     uint64
	statuscode       int
	stringstatuscode string
	senderip         string
	destination      string
	getdataretr      uint
}

// Если номер формата +Код страны и номер то оставляем как есть
// Если он формата 8 и номер, но меняем 8 на код страны по умолчанию
func (APIstruct *SMSapi) NormalizePhoneNumber(number string) (normalizednumber string, nerr error) {
	NormalizedNumber := ""
	if len(number) < int(APIstruct.Totalnumberlen) {
		return NormalizedNumber, fmt.Errorf("Номер: %s слишком короткий", number)
	}
	if string(number[0]) == "+" {
		NormalizedNumber = number[1:]
	} else {
		if string(number[0]) == "8" {
			NormalizedNumber = fmt.Sprintf("%d%s", APIstruct.Defaultcountrycode, number[1:])
		} else {
			NormalizedNumber = number
		}
	}
	return NormalizedNumber, nil
}

func (APIstruct *SMSapi) QStatus(ctx context.Context, wg *sync.WaitGroup) {
	if APIstruct.initialized == false {
		wg.Done()
		return
	}
	switch APIstruct.smsprovicerIndex {
	case 0:
		APIstruct.mtsQStatus(ctx, wg)
	case 1:
		APIstruct.dsQStatus(ctx, wg)
	}

}

func (APIstruct *SMSapi) SendSMS(ClienName string, PhoneNumber string, Data string) error {
	if APIstruct.initialized == false {
		return fmt.Errorf("SMSapi not initialized")
	}
	err := fmt.Errorf("no provider selected")
	switch APIstruct.smsprovicerIndex {
	case 0:
		err = APIstruct.sendMessageMTS(ClienName, PhoneNumber, Data)
	case 1:
		err = APIstruct.sendMessageDS(ClienName, PhoneNumber, Data)
	}
	return err
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

	switch strings.TrimSpace(strings.ToLower(APIstruct.SMSProviderName)) {
	case SMS_PROVIDER_STRINGNAME_MTS:
		APIstruct.smsprovicerIndex = 0
		APIstruct.sendurl = MTSSENDURL
		APIstruct.statusurl = MTSSTATUSURL
		break
	case SMS_PROVIDER_STRINGNAME_DS:
		APIstruct.smsprovicerIndex = 1
		APIstruct.sendurl = DSSSENDURL
		APIstruct.statusurl = DSSTATUSURL
		break
	default:
		return fmt.Errorf("SMS provider not supported")
	}

	if APIstruct.HttTimeoutSeconds == 0 || APIstruct.HttTimeoutSeconds > HTTP_MAX_TIMEOUT_SEC {
		APIstruct.HttTimeoutSeconds = HTTP_DEFAULT_TIMEOUT_SEC
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
