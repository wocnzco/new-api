package relay

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"one-api/common"
	"one-api/constant"
	"one-api/dto"
	"one-api/model"
	relayconstant "one-api/relay/constant"
	"one-api/service"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

var DefaultModelPrice = map[string]float64{
	"mj_imagine":     0.1,
	"mj_variation":   0.1,
	"mj_reroll":      0.1,
	"mj_blend":       0.1,
	"mj_inpaint":     0.1,
	"mj_zoom":        0.1,
	"mj_inpaint_pre": 0,
	"mj_describe":    0.05,
	"mj_upscale":     0.05,
	"swap_face":      0.05,
}

func RelayMidjourneyImage(c *gin.Context) {
	taskId := c.Param("id")
	midjourneyTask := model.GetByOnlyMJId(taskId)
	if midjourneyTask == nil {
		c.JSON(400, gin.H{
			"error": "midjourney_task_not_found",
		})
		return
	}
	resp, err := http.Get(midjourneyTask.ImageUrl)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "http_get_image_failed",
		})
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		responseBody, _ := io.ReadAll(resp.Body)
		c.JSON(resp.StatusCode, gin.H{
			"error": string(responseBody),
		})
		return
	}
	// 从Content-Type头获取MIME类型
	contentType := resp.Header.Get("Content-Type")
	if contentType == "" {
		// 如果无法确定内容类型，则默认为jpeg
		contentType = "image/jpeg"
	}
	// 设置响应的内容类型
	c.Writer.Header().Set("Content-Type", contentType)
	// 将图片流式传输到响应体
	_, err = io.Copy(c.Writer, resp.Body)
	if err != nil {
		log.Println("Failed to stream image:", err)
	}
	return
}

func RelayMidjourneyNotify(c *gin.Context) *dto.MidjourneyResponse {
	var midjRequest dto.MidjourneyDto
	err := common.UnmarshalBodyReusable(c, &midjRequest)
	if err != nil {
		return &dto.MidjourneyResponse{
			Code:        4,
			Description: "bind_request_body_failed",
			Properties:  nil,
			Result:      "",
		}
	}
	midjourneyTask := model.GetByOnlyMJId(midjRequest.MjId)
	if midjourneyTask == nil {
		return &dto.MidjourneyResponse{
			Code:        4,
			Description: "midjourney_task_not_found",
			Properties:  nil,
			Result:      "",
		}
	}
	midjourneyTask.Progress = midjRequest.Progress
	midjourneyTask.PromptEn = midjRequest.PromptEn
	midjourneyTask.State = midjRequest.State
	midjourneyTask.SubmitTime = midjRequest.SubmitTime
	midjourneyTask.StartTime = midjRequest.StartTime
	midjourneyTask.FinishTime = midjRequest.FinishTime
	midjourneyTask.ImageUrl = midjRequest.ImageUrl
	midjourneyTask.Status = midjRequest.Status
	midjourneyTask.FailReason = midjRequest.FailReason
	err = midjourneyTask.Update()
	if err != nil {
		return &dto.MidjourneyResponse{
			Code:        4,
			Description: "update_midjourney_task_failed",
		}
	}

	return nil
}

func coverMidjourneyTaskDto(c *gin.Context, originTask *model.Midjourney) (midjourneyTask dto.MidjourneyDto) {
	midjourneyTask.MjId = originTask.MjId
	midjourneyTask.Progress = originTask.Progress
	midjourneyTask.PromptEn = originTask.PromptEn
	midjourneyTask.State = originTask.State
	midjourneyTask.SubmitTime = originTask.SubmitTime
	midjourneyTask.StartTime = originTask.StartTime
	midjourneyTask.FinishTime = originTask.FinishTime
	midjourneyTask.ImageUrl = ""
	if originTask.ImageUrl != "" {
		midjourneyTask.ImageUrl = common.ServerAddress + "/mj/image/" + originTask.MjId
		if originTask.Status != "SUCCESS" {
			midjourneyTask.ImageUrl += "?rand=" + strconv.FormatInt(time.Now().UnixNano(), 10)
		}
	}
	midjourneyTask.Status = originTask.Status
	midjourneyTask.FailReason = originTask.FailReason
	midjourneyTask.Action = originTask.Action
	midjourneyTask.Description = originTask.Description
	midjourneyTask.Prompt = originTask.Prompt
	if originTask.Buttons != "" {
		var buttons []dto.ActionButton
		err := json.Unmarshal([]byte(originTask.Buttons), &buttons)
		if err == nil {
			midjourneyTask.Buttons = buttons
		}
	}
	if originTask.Properties != "" {
		var properties dto.Properties
		err := json.Unmarshal([]byte(originTask.Properties), &properties)
		if err == nil {
			midjourneyTask.Properties = &properties
		}
	}
	return
}

func RelayMidjournneyModal(c *gin.Context) *dto.MidjourneyResponse {
	userId := c.GetInt("id")
	var midjRequest dto.MidjourneyRequest
	err := common.UnmarshalBodyReusable(c, &midjRequest)
	if err != nil {
		return service.MidjourneyErrorWrapper(constant.MjRequestError, "bind_request_body_failed")
	}
	originTask := model.GetByMJId(userId, midjRequest.TaskId)
	if originTask == nil {
		return service.MidjourneyErrorWrapper(constant.MjRequestError, "task_no_found")
	}

	respBody, err := json.Marshal(midjRequest)
	if err != nil {
		return service.MidjourneyErrorWrapper(constant.MjRequestError, "unmarshal_response_body_failed")
	}
	c.Writer.Header().Set("Content-Type", "application/json")
	_, err = io.Copy(c.Writer, bytes.NewBuffer(respBody))
	if err != nil {
		return service.MidjourneyErrorWrapper(constant.MjRequestError, "copy_response_body_failed")
	}
	return nil

}

func RelayMidjourneyTask(c *gin.Context, relayMode int) *dto.MidjourneyResponse {
	userId := c.GetInt("id")
	var err error
	var respBody []byte
	switch relayMode {
	case relayconstant.RelayModeMidjourneyTaskFetch:
		taskId := c.Param("id")
		originTask := model.GetByMJId(userId, taskId)
		if originTask == nil {
			return &dto.MidjourneyResponse{
				Code:        4,
				Description: "task_no_found",
			}
		}
		midjourneyTask := coverMidjourneyTaskDto(c, originTask)
		respBody, err = json.Marshal(midjourneyTask)
		if err != nil {
			return &dto.MidjourneyResponse{
				Code:        4,
				Description: "unmarshal_response_body_failed",
			}
		}
	case relayconstant.RelayModeMidjourneyTaskFetchByCondition:
		var condition = struct {
			IDs []string `json:"ids"`
		}{}
		err = c.BindJSON(&condition)
		if err != nil {
			return &dto.MidjourneyResponse{
				Code:        4,
				Description: "do_request_failed",
			}
		}
		var tasks []dto.MidjourneyDto
		if len(condition.IDs) != 0 {
			originTasks := model.GetByMJIds(userId, condition.IDs)
			for _, originTask := range originTasks {
				midjourneyTask := coverMidjourneyTaskDto(c, originTask)
				tasks = append(tasks, midjourneyTask)
			}
		}
		if tasks == nil {
			tasks = make([]dto.MidjourneyDto, 0)
		}
		respBody, err = json.Marshal(tasks)
		if err != nil {
			return &dto.MidjourneyResponse{
				Code:        4,
				Description: "unmarshal_response_body_failed",
			}
		}
	}

	c.Writer.Header().Set("Content-Type", "application/json")

	_, err = io.Copy(c.Writer, bytes.NewBuffer(respBody))
	if err != nil {
		return &dto.MidjourneyResponse{
			Code:        4,
			Description: "copy_response_body_failed",
		}
	}
	return nil
}

func RelayMidjourneySubmit(c *gin.Context, relayMode int) *dto.MidjourneyResponse {
	imageModel := "midjourney"

	tokenId := c.GetInt("token_id")
	channelType := c.GetInt("channel")
	userId := c.GetInt("id")
	group := c.GetString("group")
	channelId := c.GetInt("channel_id")
	consumeQuota := true
	var midjRequest dto.MidjourneyRequest
	err := common.UnmarshalBodyReusable(c, &midjRequest)
	if err != nil {
		return service.MidjourneyErrorWrapper(constant.MjRequestError, "bind_request_body_failed")
	}

	if relayMode == relayconstant.RelayModeMidjourneyAction { // midjourney plus，需要从customId中获取任务信息
		mjErr := coverPlusActionToNormalAction(&midjRequest)
		if mjErr != nil {
			return mjErr
		}
		relayMode = relayconstant.RelayModeMidjourneyChange
	}

	if relayMode == relayconstant.RelayModeMidjourneyImagine { //绘画任务，此类任务可重复
		if midjRequest.Prompt == "" {
			return service.MidjourneyErrorWrapper(constant.MjRequestError, "prompt_is_required")
		}
		midjRequest.Action = constant.MjActionImagine
	} else if relayMode == relayconstant.RelayModeMidjourneyDescribe { //按图生文任务，此类任务可重复
		midjRequest.Action = constant.MjActionDescribe
	} else if relayMode == relayconstant.RelayModeMidjourneyShorten { //缩短任务，此类任务可重复，plus only
		midjRequest.Action = constant.MjActionShorten
	} else if relayMode == relayconstant.RelayModeMidjourneyBlend { //绘画任务，此类任务可重复
		midjRequest.Action = "BLEND"
	} else if midjRequest.TaskId != "" { //放大、变换任务，此类任务，如果重复且已有结果，远端api会直接返回最终结果
		mjId := ""
		if relayMode == relayconstant.RelayModeMidjourneyChange {
			if midjRequest.TaskId == "" {
				return service.MidjourneyErrorWrapper(constant.MjRequestError, "task_id_is_required")
			} else if midjRequest.Action == "" {
				return service.MidjourneyErrorWrapper(constant.MjRequestError, "action_is_required")
			} else if midjRequest.Index == 0 {
				return service.MidjourneyErrorWrapper(constant.MjRequestError, "index_is_required")
			}
			//action = midjRequest.Action
			mjId = midjRequest.TaskId
		} else if relayMode == relayconstant.RelayModeMidjourneySimpleChange {
			if midjRequest.Content == "" {
				return service.MidjourneyErrorWrapper(constant.MjRequestError, "content_is_required")
			}
			params := convertSimpleChangeParams(midjRequest.Content)
			if params == nil {
				return service.MidjourneyErrorWrapper(constant.MjRequestError, "content_parse_failed")
			}
			mjId = params.ID
			midjRequest.Action = params.Action
		} else if relayMode == relayconstant.RelayModeMidjourneyModal {
			if midjRequest.MaskBase64 == "" {
				return service.MidjourneyErrorWrapper(constant.MjRequestError, "mask_base64_is_required")
			}
			mjId = midjRequest.TaskId
			midjRequest.Action = constant.MjActionInPaint
		}

		originTask := model.GetByMJId(userId, mjId)
		if originTask == nil {
			return service.MidjourneyErrorWrapper(constant.MjRequestError, "task_not_found")
		} else if originTask.Status != "SUCCESS" && relayMode != relayconstant.RelayModeMidjourneyModal {
			return service.MidjourneyErrorWrapper(constant.MjRequestError, "task_status_not_success")
		} else { //原任务的Status=SUCCESS，则可以做放大UPSCALE、变换VARIATION等动作，此时必须使用原来的请求地址才能正确处理
			channel, err := model.GetChannelById(originTask.ChannelId, false)
			if err != nil {
				return service.MidjourneyErrorWrapper(constant.MjRequestError, "get_channel_info_failed")
			}
			c.Set("base_url", channel.GetBaseURL())
			c.Set("channel_id", originTask.ChannelId)
			log.Printf("检测到此操作为放大、变换、重绘，获取原channel信息: %s,%s", strconv.Itoa(originTask.ChannelId), channel.GetBaseURL())
		}
		midjRequest.Prompt = originTask.Prompt

		if channelType == common.ChannelTypeMidjourneyPlus {
			// plus
		} else {
			// 普通版渠道

		}
	}

	if midjRequest.Action == constant.MjActionInPaintPre {
		consumeQuota = false
	}

	// map model name
	modelMapping := c.GetString("model_mapping")
	isModelMapped := false
	if modelMapping != "" {
		modelMap := make(map[string]string)
		err := json.Unmarshal([]byte(modelMapping), &modelMap)
		if err != nil {
			//return errorWrapper(err, "unmarshal_model_mapping_failed", http.StatusInternalServerError)
			return &dto.MidjourneyResponse{
				Code:        4,
				Description: "unmarshal_model_mapping_failed",
			}
		}
		if modelMap[imageModel] != "" {
			imageModel = modelMap[imageModel]
			isModelMapped = true
		}
	}

	baseURL := common.ChannelBaseURLs[channelType]
	requestURL := c.Request.URL.String()

	if c.GetString("base_url") != "" {
		baseURL = c.GetString("base_url")
	}

	//midjRequest.NotifyHook = "http://127.0.0.1:3000/mj/notify"

	fullRequestURL := fmt.Sprintf("%s%s", baseURL, requestURL)

	var requestBody io.Reader
	if isModelMapped {
		jsonStr, err := json.Marshal(midjRequest)
		if err != nil {
			return &dto.MidjourneyResponse{
				Code:        4,
				Description: "marshal_text_request_failed",
			}
		}
		requestBody = bytes.NewBuffer(jsonStr)
	} else {
		requestBody = c.Request.Body
	}

	mjAction := "mj_" + strings.ToLower(midjRequest.Action)
	modelPrice := common.GetModelPrice(mjAction, true)
	// 如果没有配置价格，则使用默认价格
	if modelPrice == -1 {
		defaultPrice, ok := DefaultModelPrice[mjAction]
		if !ok {
			modelPrice = 0.1
		} else {
			modelPrice = defaultPrice
		}
	}
	groupRatio := common.GetGroupRatio(group)
	ratio := modelPrice * groupRatio
	userQuota, err := model.CacheGetUserQuota(userId)
	if err != nil {
		return &dto.MidjourneyResponse{
			Code:        4,
			Description: err.Error(),
		}
	}
	quota := int(ratio * common.QuotaPerUnit)

	if consumeQuota && userQuota-quota < 0 {
		return &dto.MidjourneyResponse{
			Code:        4,
			Description: "quota_not_enough",
		}
	}

	req, err := http.NewRequest(c.Request.Method, fullRequestURL, requestBody)
	if err != nil {
		return &dto.MidjourneyResponse{
			Code:        4,
			Description: "create_request_failed",
		}
	}
	//req.Header.Set("ApiKey", c.Request.Header.Get("ApiKey"))
	timeout := time.Second * 30
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	// 使用带有超时的 context 创建新的请求
	req = req.WithContext(ctx)
	req.Header.Set("Content-Type", c.Request.Header.Get("Content-Type"))
	req.Header.Set("Accept", c.Request.Header.Get("Accept"))
	req.Header.Set("mj-api-secret", strings.Split(c.Request.Header.Get("Authorization"), " ")[1])
	// print request header
	//log.Printf("request header: %s", req.Header)
	//log.Printf("request body: %s", midjRequest.Prompt)

	defer cancel()
	resp, err := service.GetHttpClient().Do(req)
	if err != nil {
		return service.MidjourneyErrorWrapper(constant.MjErrorUnknown, "do_request_failed")
	}

	err = req.Body.Close()
	if err != nil {
		return service.MidjourneyErrorWrapper(constant.MjErrorUnknown, "close_request_body_failed")
	}
	err = c.Request.Body.Close()
	if err != nil {
		return service.MidjourneyErrorWrapper(constant.MjErrorUnknown, "close_request_body_failed")
	}
	var midjResponse dto.MidjourneyResponse

	defer func(ctx context.Context) {
		if consumeQuota {
			err := model.PostConsumeTokenQuota(tokenId, userQuota, quota, 0, true)
			if err != nil {
				common.SysError("error consuming token remain quota: " + err.Error())
			}
			err = model.CacheUpdateUserQuota(userId)
			if err != nil {
				common.SysError("error update user quota cache: " + err.Error())
			}
			if quota != 0 {
				tokenName := c.GetString("token_name")
				logContent := fmt.Sprintf("模型固定价格 %.2f，分组倍率 %.2f，操作 %s", modelPrice, groupRatio, midjRequest.Action)
				model.RecordConsumeLog(ctx, userId, channelId, 0, 0, imageModel, tokenName, quota, logContent, tokenId, userQuota, 0, false)
				model.UpdateUserUsedQuotaAndRequestCount(userId, quota)
				channelId := c.GetInt("channel_id")
				model.UpdateChannelUsedQuota(channelId, quota)
			}
		}
	}(c.Request.Context())

	responseBody, err := io.ReadAll(resp.Body)

	if err != nil {
		return service.MidjourneyErrorWrapper(constant.MjErrorUnknown, "read_response_body_failed")
	}
	err = resp.Body.Close()
	if err != nil {
		return service.MidjourneyErrorWrapper(constant.MjErrorUnknown, "close_response_body_failed")
	}
	if resp.StatusCode != 200 {
		return service.MidjourneyErrorWrapper(constant.MjErrorUnknown, "unexpected_response_status")
	}
	err = json.Unmarshal(responseBody, &midjResponse)
	log.Printf("responseBody: %s", string(responseBody))
	log.Printf("midjResponse: %v", midjResponse)
	if err != nil {
		return service.MidjourneyErrorWrapper(constant.MjErrorUnknown, "unmarshal_response_body_failed")
	}

	// 文档：https://github.com/novicezk/midjourney-proxy/blob/main/docs/api.md
	//1-提交成功
	// 21-任务已存在（处理中或者有结果了） {"code":21,"description":"任务已存在","result":"0741798445574458","properties":{"status":"SUCCESS","imageUrl":"https://xxxx"}}
	// 22-排队中 {"code":22,"description":"排队中，前面还有1个任务","result":"0741798445574458","properties":{"numberOfQueues":1,"discordInstanceId":"1118138338562560102"}}
	// 23-队列已满，请稍后再试 {"code":23,"description":"队列已满，请稍后尝试","result":"14001929738841620","properties":{"discordInstanceId":"1118138338562560102"}}
	// 24-prompt包含敏感词 {"code":24,"description":"可能包含敏感词","properties":{"promptEn":"nude body","bannedWord":"nude"}}
	// other: 提交错误，description为错误描述
	midjourneyTask := &model.Midjourney{
		UserId:      userId,
		Code:        midjResponse.Code,
		Action:      midjRequest.Action,
		MjId:        midjResponse.Result,
		Prompt:      midjRequest.Prompt,
		PromptEn:    "",
		Description: midjResponse.Description,
		State:       "",
		SubmitTime:  time.Now().UnixNano() / int64(time.Millisecond),
		StartTime:   0,
		FinishTime:  0,
		ImageUrl:    "",
		Status:      "",
		Progress:    "0%",
		FailReason:  "",
		ChannelId:   c.GetInt("channel_id"),
		Quota:       quota,
	}

	if midjResponse.Code != 1 && midjResponse.Code != 21 && midjResponse.Code != 22 {
		//非1-提交成功,21-任务已存在和22-排队中，则记录错误原因
		midjourneyTask.FailReason = midjResponse.Description
		consumeQuota = false
	}

	if midjResponse.Code == 21 { //21-任务已存在（处理中或者有结果了）
		// 将 properties 转换为一个 map
		properties, ok := midjResponse.Properties.(map[string]interface{})
		if ok {
			imageUrl, ok1 := properties["imageUrl"].(string)
			status, ok2 := properties["status"].(string)
			if ok1 && ok2 {
				midjourneyTask.ImageUrl = imageUrl
				midjourneyTask.Status = status
				if status == "SUCCESS" {
					midjourneyTask.Progress = "100%"
					midjourneyTask.StartTime = time.Now().UnixNano() / int64(time.Millisecond)
					midjourneyTask.FinishTime = time.Now().UnixNano() / int64(time.Millisecond)
					midjResponse.Code = 1
				}
			}
		}
		//修改返回值
		newBody := strings.Replace(string(responseBody), `"code":21`, `"code":1`, -1)
		responseBody = []byte(newBody)
	}

	err = midjourneyTask.Insert()
	if err != nil {
		return &dto.MidjourneyResponse{
			Code:        4,
			Description: "insert_midjourney_task_failed",
		}
	}

	if midjResponse.Code == 22 { //22-排队中，说明任务已存在
		//修改返回值
		newBody := strings.Replace(string(responseBody), `"code":22`, `"code":1`, -1)
		responseBody = []byte(newBody)
	}

	resp.Body = io.NopCloser(bytes.NewBuffer(responseBody))

	for k, v := range resp.Header {
		c.Writer.Header().Set(k, v[0])
	}
	c.Writer.WriteHeader(resp.StatusCode)

	_, err = io.Copy(c.Writer, resp.Body)
	if err != nil {
		return &dto.MidjourneyResponse{
			Code:        4,
			Description: "copy_response_body_failed",
		}
	}
	err = resp.Body.Close()
	if err != nil {
		return &dto.MidjourneyResponse{
			Code:        4,
			Description: "close_response_body_failed",
		}
	}
	return nil
}

type taskChangeParams struct {
	ID     string
	Action string
	Index  int
}

func convertSimpleChangeParams(content string) *taskChangeParams {
	split := strings.Split(content, " ")
	if len(split) != 2 {
		return nil
	}

	action := strings.ToLower(split[1])
	changeParams := &taskChangeParams{}
	changeParams.ID = split[0]

	if action[0] == 'u' {
		changeParams.Action = "UPSCALE"
	} else if action[0] == 'v' {
		changeParams.Action = "VARIATION"
	} else if action == "r" {
		changeParams.Action = "REROLL"
		return changeParams
	} else {
		return nil
	}

	index, err := strconv.Atoi(action[1:2])
	if err != nil || index < 1 || index > 4 {
		return nil
	}
	changeParams.Index = index
	return changeParams
}

func coverPlusActionToNormalAction(midjRequest *dto.MidjourneyRequest) *dto.MidjourneyResponse {
	// "customId": "MJ::JOB::upsample::2::3dbbd469-36af-4a0f-8f02-df6c579e7011"
	customId := midjRequest.CustomId
	if customId == "" {
		return service.MidjourneyErrorWrapper(constant.MjRequestError, "custom_id_is_required")
	}
	splits := strings.Split(customId, "::")
	var action string
	if splits[1] == "JOB" {
		action = splits[2]
	} else {
		action = splits[1]
	}

	if action == "" {
		return service.MidjourneyErrorWrapper(constant.MjRequestError, "unknown_action")
	}
	if strings.Contains(action, "upsample") {
		index, err := strconv.Atoi(splits[3])
		if err != nil {
			return service.MidjourneyErrorWrapper(constant.MjRequestError, "index_parse_failed")
		}
		midjRequest.Index = index
		midjRequest.Action = constant.MjActionUpscale
	} else if strings.Contains(action, "variation") {
		midjRequest.Action = constant.MjActionVariation
		midjRequest.Index = 1
	} else if strings.Contains(action, "pan") {
		midjRequest.Action = constant.MjActionVariation
		midjRequest.Index = 1
	} else if action == "Outpaint" || action == "CustomZoom" {
		midjRequest.Action = constant.MjActionZoom
		midjRequest.Index = 1
	} else if action == "Inpaint" {
		midjRequest.Action = constant.MjActionInPaintPre
		midjRequest.Index = 1
	} else {
		return service.MidjourneyErrorWrapper(constant.MjRequestError, "unknown_action")
	}
	return nil
}
