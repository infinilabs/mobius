package main

// Transitional aliases (plan 6.4e): shared tool implementations live in
// internal/tools.

import (
	"mobius/internal/tools"
)

type (
	WatermarkEngine     = tools.WatermarkEngine
	WatermarkTaskDriver = tools.WatermarkTaskDriver
	WriteHTMLReport     = tools.WriteHTMLReport
)

var (
	NewWatermarkEngine                = tools.NewWatermarkEngine
	NewWatermarkTaskDriver            = tools.NewWatermarkTaskDriver
	isVideoFile                       = tools.IsVideoFile
	execPlayableLoadReferenceGameTool = tools.ExecPlayableLoadReferenceGameTool
	execPlayableGetTrackingSDKTool    = tools.ExecPlayableGetTrackingSDKTool
	execPlayableGetWebAudioSFXTool    = tools.ExecPlayableGetWebAudioSFXTool
	execPlayableWriteHTMLTool         = tools.ExecPlayableWriteHTMLTool
	execSaveUploadToAssetsTool        = tools.ExecSaveUploadToAssetsTool
	execGenerateImageTool             = tools.ExecGenerateImageTool
	execGenerateAudioTool             = tools.ExecGenerateAudioTool
	execPublishPlayableAdTool         = tools.ExecPublishPlayableAdTool
	execTagMediaTool                  = tools.ExecTagMediaTool
	execGetTagResultsTool             = tools.ExecGetTagResultsTool
	execQueryTagsTool                 = tools.ExecQueryTagsTool
	execWatermarkAssetsTool           = tools.ExecWatermarkAssetsTool
	execVerifyWatermarkTool           = tools.ExecVerifyWatermarkTool
	resolvePlayableProjectID          = tools.ResolvePlayableProjectID
	playableProjectDir                = tools.PlayableProjectDir
)
