package dependencies

import "github.com/centrifugal/gocent/v3"

const centrifugoAddress = "http://localhost:8000/api"

const centrifugoApiKey = "0Bmi8sRKc-lpnA77ti0AW_P8vWIn8FDgfVkknlv-xIclvAlIX0DGIC95bRy_K6PXvP1Yb-J3m2mTAd2rVoTLnQ"

func ConnectCentrifugoClient() *gocent.Client {
	config := gocent.Config{Addr: centrifugoAddress, Key: centrifugoApiKey}
	client := gocent.New(config)
	return client
}