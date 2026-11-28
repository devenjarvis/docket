package demo

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/devenjarvis/docket/internal/config"
	"github.com/devenjarvis/docket/internal/model"
	"github.com/devenjarvis/docket/internal/repo"
)

var _ repo.Brancher = Checkout{}

// The scripted agent's commits on acme/ledger#318 and acme/web-app#2230.
const (
	commitRename = "demo-fix-rename-currencymeta"
	commitNilMap = "demo-fix-nil-map-on-refresh"
	commitMerge  = "demo-merge-main-into-lib-dates"
	// commitRefundFix answers both findings of the self-review of #4140.
	commitRefundFix = "demo-fix-refund-idempotency-and-log"
)

var patches = map[string]string{
	commitRefundFix: `diff --git a/src/refunds/client.ts b/src/refunds/client.ts
--- a/src/refunds/client.ts
+++ b/src/refunds/client.ts
@@ -9,9 +9,8 @@ export class RefundClient {
   async refund(req: RefundRequest): Promise<RefundResult> {
     for (let attempt = 1; ; attempt++) {
-      console.log("refund attempt", attempt, req);
       try {
-        return await this.gateway.post("/refunds", req);
+        return await this.gateway.post("/refunds", req, { idempotencyKey: ` + "`refund-${req.id}`" + ` });
       } catch (err) {
         if (!(err instanceof GatewayTimeout) || attempt >= REFUND_MAX_ATTEMPTS) {
`,
	commitRename: `diff --git a/src/currency/cache.go b/src/currency/cache.go
--- a/src/currency/cache.go
+++ b/src/currency/cache.go
@@ -12,8 +12,8 @@ import (
-// curMeta holds the metadata loaded at startup.
-var curMeta map[string]Meta
+// currencyMeta holds the metadata loaded at startup.
+var currencyMeta map[string]Meta
@@ -20,4 +20,4 @@ func Refresh(db *sql.DB) error {
-		curMeta[m.Code] = m
+		currencyMeta[m.Code] = m
`,
	commitNilMap: `diff --git a/src/currency/cache.go b/src/currency/cache.go
--- a/src/currency/cache.go
+++ b/src/currency/cache.go
@@ -17,6 +17,9 @@ func Refresh(db *sql.DB) error {
 	rows, err := db.Query(metaQuery)
 	if err != nil {
 		return err
 	}
+	if currencyMeta == nil {
+		currencyMeta = make(map[string]Meta)
+	}
 	for rows.Next() {
`,
	commitMerge: `diff --git a/src/components/DatePicker.tsx b/src/components/DatePicker.tsx
--- a/src/components/DatePicker.tsx
+++ b/src/components/DatePicker.tsx
@@ -1,6 +1,6 @@
-import { startOfDay, toUTC } from "./dateUtils";
+import { startOfDay, toUTC } from "../lib/dates";
 
 export function DatePicker(props: Props) {
-  const day = startOfDay(toUTC(props.value, props.zone));
+  const day = startOfDay(toUTC(props.value, props.zone ?? "UTC"));
`,
}

func (c Checkout) PrepareBranch(_ context.Context, pr model.PR, st model.MergeState) (repo.Branch, error) {
	b := repo.BranchLayout(c.Home, pr)
	for _, d := range []string{b.Work, b.Scratch, b.Out} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return b, err
		}
	}
	b.Start = pr.HeadSHA
	b.Remote = "https://github.com/" + st.HeadRepo + ".git"
	return b, nil
}

// OnTop accepts the scripted agent's commits and Docket's reverts, which is
// all a demo branch ever holds.
func (Checkout) OnTop(_ context.Context, b repo.Branch, sha string) bool {
	return sha == b.Start || strings.HasPrefix(sha, "demo-")
}

func (Checkout) Patch(_ context.Context, _ repo.Branch, _, to string) (string, error) {
	if p, ok := patches[strings.TrimPrefix(to, "demo-revert-")]; ok {
		return p, nil
	}
	return "", fmt.Errorf("the demo has no commit %s", to)
}

// Push moves the demo pull request's head, and the checks start again. They
// pass once the nil map is fixed and fail until then.
func (c Checkout) Push(_ context.Context, _ repo.Branch, pr model.PR, _, to string) error {
	in := GitHub{Home: c.Home}.state()
	in.mu.Lock()
	defer in.mu.Unlock()
	o := in.find(pr.Owner, pr.Repo, pr.Number)
	if o == nil {
		return errors.New("only your own pull requests can be pushed to in the demo")
	}
	if pr.HeadSHA != "" && o.pr.HeadSHA != pr.HeadSHA && !strings.HasPrefix(o.pr.HeadSHA, "demo-") {
		return fmt.Errorf("%w: the demo branch is at %s", repo.ErrMoved, o.pr.HeadSHA)
	}
	switch to {
	case commitNilMap:
		o.fixed = true
	case "demo-revert-" + commitNilMap:
		o.fixed = false
	case commitMerge:
		o.st.Mergeable, o.st.MergeStateStatus = "MERGEABLE", "BLOCKED"
	}
	o.pr.HeadSHA = to
	for i := range o.checks {
		o.checks[i].Status, o.checks[i].Conclusion, o.checks[i].StartedAt, o.checks[i].CompletedAt = "IN_PROGRESS", "", time.Now(), time.Time{}
	}
	o.rerunAt = time.Now()
	o.touch()
	return config.WriteJSON(filepath.Join(c.Home, "posted", fmt.Sprintf("push-%s-%d-%d.json", pr.Repo, pr.Number, time.Now().UnixNano())), map[string]string{"to": to})
}

func (Checkout) Resolve(_ context.Context, _ repo.Branch, ref string) (string, error) {
	return ref, nil
}

func (Checkout) Revert(_ context.Context, _ repo.Branch, _, sha string) (string, error) {
	return "demo-revert-" + sha, nil
}

func (Checkout) ReleaseBranch(_ context.Context, _ model.PR, b repo.Branch) {
	_ = os.RemoveAll(b.Scratch)
}
