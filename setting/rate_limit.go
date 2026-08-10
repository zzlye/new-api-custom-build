package setting

import (
	"fmt"
	"math"
	"strings"
	"sync"

	"github.com/QuantumNous/new-api/common"
)

var ModelRequestRateLimitEnabled = false
var ModelRequestRateLimitDurationMinutes = 1
var ModelRequestRateLimitCount = 0
var ModelRequestRateLimitSuccessCount = 1000
var ModelRequestRateLimitGroup = map[string][2]int{}
var ModelRequestRateLimitModel = map[string][2]int{}
var ModelRequestRateLimitMutex sync.RWMutex

func ModelRequestRateLimitGroup2JSONString() string {
	ModelRequestRateLimitMutex.RLock()
	defer ModelRequestRateLimitMutex.RUnlock()

	jsonBytes, err := common.Marshal(ModelRequestRateLimitGroup)
	if err != nil {
		common.SysLog("error marshalling model request group rate limit: " + err.Error())
	}
	return string(jsonBytes)
}

func UpdateModelRequestRateLimitGroupByJSONString(jsonStr string) error {
	limits, err := parseModelRequestRateLimitMap(jsonStr, "group")
	if err != nil {
		return err
	}

	ModelRequestRateLimitMutex.Lock()
	defer ModelRequestRateLimitMutex.Unlock()
	ModelRequestRateLimitGroup = limits
	return nil
}

func GetGroupRateLimit(group string) (totalCount, successCount int, found bool) {
	ModelRequestRateLimitMutex.RLock()
	defer ModelRequestRateLimitMutex.RUnlock()

	if ModelRequestRateLimitGroup == nil {
		return 0, 0, false
	}

	limits, found := ModelRequestRateLimitGroup[group]
	if !found {
		return 0, 0, false
	}
	return limits[0], limits[1], true
}

func CheckModelRequestRateLimitGroup(jsonStr string) error {
	_, err := parseModelRequestRateLimitMap(jsonStr, "group")
	return err
}

func ModelRequestRateLimitModel2JSONString() string {
	ModelRequestRateLimitMutex.RLock()
	defer ModelRequestRateLimitMutex.RUnlock()

	jsonBytes, err := common.Marshal(ModelRequestRateLimitModel)
	if err != nil {
		common.SysLog("error marshalling model request model rate limit: " + err.Error())
	}
	return string(jsonBytes)
}

func UpdateModelRequestRateLimitModelByJSONString(jsonStr string) error {
	limits, err := parseModelRequestRateLimitMap(jsonStr, "model")
	if err != nil {
		return err
	}

	ModelRequestRateLimitMutex.Lock()
	defer ModelRequestRateLimitMutex.Unlock()
	ModelRequestRateLimitModel = limits
	return nil
}

func GetModelRateLimit(model string) (totalCount, successCount int, found bool) {
	ModelRequestRateLimitMutex.RLock()
	defer ModelRequestRateLimitMutex.RUnlock()

	if ModelRequestRateLimitModel == nil {
		return 0, 0, false
	}

	limits, found := ModelRequestRateLimitModel[model]
	if !found {
		return 0, 0, false
	}
	return limits[0], limits[1], true
}

func CheckModelRequestRateLimitModel(jsonStr string) error {
	_, err := parseModelRequestRateLimitMap(jsonStr, "model")
	return err
}

func parseModelRequestRateLimitMap(jsonStr, scopeType string) (map[string][2]int, error) {
	rawLimits := make(map[string][]int)
	if err := common.UnmarshalJsonStr(jsonStr, &rawLimits); err != nil {
		return nil, err
	}
	if rawLimits == nil {
		return nil, fmt.Errorf("%s rate limits must be a JSON object", scopeType)
	}

	limitsByScope := make(map[string][2]int, len(rawLimits))
	for scope, limits := range rawLimits {
		if strings.TrimSpace(scope) == "" {
			return nil, fmt.Errorf("%s rate limit target cannot be empty", scopeType)
		}
		if len(limits) != 2 {
			return nil, fmt.Errorf("%s %s rate limit must contain exactly two values", scopeType, scope)
		}
		if limits[0] < 0 || limits[1] < 1 {
			return nil, fmt.Errorf("%s %s has invalid rate limit values: [%d, %d]", scopeType, scope, limits[0], limits[1])
		}
		if limits[0] > math.MaxInt32 || limits[1] > math.MaxInt32 {
			return nil, fmt.Errorf("%s %s [%d, %d] has max rate limits value 2147483647", scopeType, scope, limits[0], limits[1])
		}
		limitsByScope[scope] = [2]int{limits[0], limits[1]}
	}

	return limitsByScope, nil
}
