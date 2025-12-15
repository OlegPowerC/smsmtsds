package smsmtsds

import (
	"fmt"
	"log"
	"sync"
)

// Максимально допустимое время на установку TCP сессии, включая разрешение имени (DNS запрос) и обмен данными
const TIMEOUT_MAXSEC = 10
const TIMEOUT_DEFAULT = 5

type SMSapi struct {
	Sendurl            string
	Stturl             string
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

func (APIstruct *SMSapi) SendSMS(ClienName string, PhoneNumber string, Data string) error {
	err := APIstruct.sendMessageDS(ClienName, PhoneNumber, Data)
	return err
}
