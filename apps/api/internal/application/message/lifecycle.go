package applicationmessage

import (
	domainmessage "github.com/shiyudesu/frux/internal/domain/message"
)

func LifecycleMessageContent(
	notification domainmessage.LifecycleNotification,
) (string, string, error) {
	if err := domainmessage.ValidateLifecycle(
		notification.Stage, notification.Result,
		notification.ReasonCode, notification.VideoID,
	); err != nil {
		return "", "", err
	}
	switch notification.Stage {
	case domainmessage.LifecycleStageSubmitted:
		return "视频已提交审核", "你的视频已提交，正在等待审核。", nil
	case domainmessage.LifecycleStageReview:
		if notification.Result == domainmessage.LifecycleResultApproved {
			return "视频审核通过", "你的视频已通过审核，当前尚未公开，可在作品页查看处理状态或可见性设置。", nil
		}
		return "视频审核未通过", "你的视频未通过审核，原因：" +
			lifecycleReasonLabel(notification.ReasonCode) + "。", nil
	case domainmessage.LifecycleStageMediaProcessing:
		return "视频处理失败", "视频媒体处理失败，请重新上传或稍后重试。", nil
	case domainmessage.LifecycleStagePublished:
		return "视频已发布", "你的视频审核通过并已公开发布。", nil
	case domainmessage.LifecycleStageEnforcement:
		return "视频已下架", "你的视频已下架，原因：" +
			lifecycleReasonLabel(notification.ReasonCode) + "。", nil
	case domainmessage.LifecycleStageRestoration:
		return "视频已恢复", "你的视频已恢复，可按当前可见性继续访问。", nil
	default:
		return "", "", domainmessage.ErrInvalidLifecycle
	}
}

func lifecycleReasonLabel(reason string) string {
	switch reason {
	case "sexual_content":
		return "色情内容"
	case "graphic_violence":
		return "血腥暴力"
	case "hate":
		return "仇恨内容"
	case "harassment":
		return "骚扰内容"
	case "self_harm":
		return "自残内容"
	case "illegal_activity":
		return "违法活动"
	case "spam":
		return "垃圾内容"
	case "other_policy_violation":
		return "其他策略违规"
	case "manual_enforcement":
		return "运营处置"
	case "policy_violation":
		return "违反平台规则"
	case "compliance_restored":
		return "已确认合规"
	default:
		return "处理状态变化"
	}
}
