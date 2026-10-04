package photosdb

import "time"

const appleEpoch = 978307200

func AppleTime(seconds float64) time.Time {
	whole := int64(seconds)
	nano := int64((seconds - float64(whole)) * 1e9)
	return time.Unix(appleEpoch+whole, nano).UTC()
}
