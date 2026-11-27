package review

import (
	"strings"
	"testing"

	"github.com/devenjarvis/docket/internal/model"
)

func TestNewsAboutOwnPullRequests(t *testing.T) {
	thread := func(authors ...string) model.Thread {
		th := model.Thread{ID: "t"}
		for i, a := range authors {
			th.Comments = append(th.Comments, model.Comment{ID: a + string(rune('0'+i)), Author: a})
		}
		return th
	}
	base := &model.Mine{Lane: model.LaneWaiting, Conversation: model.Conversation{Threads: []model.Thread{thread("maya")}}}
	cases := []struct {
		name string
		next model.Mine
		want string
	}{
		{"nothing changed", *base, ""},
		{"became ready", model.Mine{Lane: model.LaneReady, Conversation: base.Conversation}, "ready to merge"},
		{"needs you", model.Mine{Lane: model.LaneNeedsYou, Conversation: base.Conversation}, "needs you"},
		{"own reply is not news", model.Mine{Lane: model.LaneWaiting, Conversation: model.Conversation{Threads: []model.Thread{thread("maya", "me")}}}, ""},
		{"their reply is news", model.Mine{Lane: model.LaneWaiting, Conversation: model.Conversation{Threads: []model.Thread{thread("maya", "maya")}}}, "maya commented"},
		{"a review is news", model.Mine{Lane: model.LaneWaiting, Conversation: model.Conversation{Threads: base.Conversation.Threads, Reviews: []model.ReviewEvent{{ID: "r1", Author: "sam", State: "APPROVED"}}}}, "sam approved"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			lane, activity := news("me", base, &c.next)
			got := strings.TrimSpace(lane + " " + activity)
			if c.want == "" && got != "" || c.want != "" && !strings.Contains(got, c.want) {
				t.Errorf("news = %q, want it to say %q", got, c.want)
			}
		})
	}
	if lane, activity := news("me", nil, base); lane+activity != "" {
		got := lane + activity
		t.Errorf("a pull request seen for the first time is not news: %q", got)
	}
}
