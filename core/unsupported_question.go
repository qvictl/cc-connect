package core

import "strings"

// Unknown question-bearing permission shapes must not be answered as boolean
// approvals. In particular, an adapter may emit questions but have no answer
// transport; collecting answers here would falsely imply they were delivered.
func isUnsupportedQuestionRequest(event Event) bool {
	return event.Type == EventPermissionRequest && len(event.Questions) > 0 && !isAskQuestionRequest(event)
}

func (e *Engine) sendUnsupportedQuestion(p Platform, replyCtx any, event Event) {
	questions := make([]string, 0, len(event.Questions))
	for _, q := range event.Questions {
		questions = append(questions, q.Question)
	}
	e.send(p, replyCtx, e.i18n.Tf(MsgUnsupportedQuestion, strings.Join(questions, "\n")))
}
