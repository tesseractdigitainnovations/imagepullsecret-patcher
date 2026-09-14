package config

import (
	"os"
	"strconv"
	"time"
)

// Reference:
// https://www.gmarik.info/blog/2019/12-factor-golang-flag-package/

// LookupEnvOrString returns the value of the environment variable key,
// or defaultVal when it is not set.
func LookupEnvOrString(key string, defaultVal string) string {
	if val, ok := os.LookupEnv(key); ok {
		return val
	}
	return defaultVal
}

// LookupEnvOrBool returns the value of the environment variable key parsed as
// a bool, or defaultVal when it is not set or cannot be parsed.
func LookupEnvOrBool(key string, defaultVal bool) bool {
	str, ok := os.LookupEnv(key)
	if !ok {
		return defaultVal
	}
	val, err := strconv.ParseBool(str)
	if err != nil {
		return defaultVal
	}
	return val
}

// LookupEnvOrDuration returns the value of the environment variable key parsed
// as a time.Duration, or defaultVal when it is not set or cannot be parsed.
func LookupEnvOrDuration(key string, defaultVal time.Duration) time.Duration {
	str, ok := os.LookupEnv(key)
	if !ok {
		return defaultVal
	}
	val, err := time.ParseDuration(str)
	if err != nil {
		return defaultVal
	}
	return val
}
