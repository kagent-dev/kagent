# Requirements

## Q&A Record

**Q1:** Should we fix only the CronJobs page, or also fix the same pattern in all other affected pages (git/page.tsx, models/page.tsx, plugins.ts, models/new/page.tsx)?

**A1:** Fix only the CronJobs page.

**Q2:** Should the fix be applied on both layers (backend: ensure non-nil slice in the CronJobs list handler + UI: treat missing data as empty array), or just one side?

**A2:** Both layers — backend and UI.

**Q3:** For the backend fix, should we initialize the nil slice only in the CronJobs handler, or also remove `omitempty` from `StandardResponse.Data` to prevent this class of bug for all endpoints?

**A3:** Only in the CronJobs handler.

**Q4:** Are there any additional requirements for the empty state UI beyond what currently exists (Clock icon + "No cron jobs found. Create one to get started." message), or is the current empty state design sufficient once it's reachable?

**A4:** Current empty state design is sufficient — no changes needed.


