export const concepts = [
  {
    term: "general",
    def: "The human - you. Talks only to the commander; never manages soldiers directly.",
  },
  {
    term: "commander",
    def: "The orchestrator you run. Reads a mission, dispatches soldiers, and tracks their progress.",
  },
  {
    term: "soldier",
    def: "A subagent the commander runs. Each one works a task independently, inside its own camp.",
  },
  {
    term: "camp",
    def: "A soldier's isolated workspace - its own worktree, so parallel soldiers never collide.",
  },
  {
    term: "sentinel",
    def: "Watches running soldiers and flags the commander when one needs human input.",
  },
  {
    term: "mission / scout",
    def: "The two task types a commander can run: a mission executes work, a scout investigates and reports back.",
  },
];

export const commands = [
  { name: "init", desc: "Prepare the current project to be orchestrated by vexillum." },
  { name: "doctor", desc: "Report on the health of the vexillum environment - read-only." },
  { name: "dispatch", desc: "Dispatch a soldier (mission or scout) into an isolated camp." },
  { name: "redispatch", desc: "Re-dispatch an interrupted task from its original prompt." },
  { name: "land", desc: "Land a finished mission's work into the base branch." },
  {
    name: "ship",
    desc: "Push a finished mission through an external validation gate for a real PR.",
  },
  {
    name: "release",
    desc: "Release a soldier's camp back to the pool once its work has landed.",
  },
  { name: "sentinel", desc: "Watch dispatched soldiers and record status changes." },
  { name: "upgrade", desc: "Refresh an already-initialized project's scaffold." },
];

export const featureClusters = [
  {
    slug: "isolation",
    name: "Isolation",
    blurb:
      "Soldiers never touch your real working tree, and you're not on the hook for watching them work.",
  },
  {
    slug: "recovery",
    name: "Recovery",
    blurb:
      "Things restart, panes close, soldiers get stuck. None of that should cost you the state you already had.",
  },
  {
    slug: "shipping",
    name: "Shipping",
    blurb:
      "Two ways for a mission's work to leave its camp, both explicit, neither one silent.",
  },
  {
    slug: "environment",
    name: "Environment",
    blurb:
      "What vexillum checks before you dispatch anything, and what it never touches without you asking.",
  },
];

export const features = [
  {
    term: "Isolated camps",
    cluster: "isolation",
    icon: "Tent",
    commands: ["dispatch"],
    def: "Every soldier gets its own git worktree - a camp - branched off your project. It edits, commits, and runs tests there without ever touching your real checkout, so you can keep working (or dispatch another soldier) while it's in flight.",
    prompt:
      "Dispatch a soldier to add rate limiting to the API - I'm still mid-review on something else, don't touch my working tree.",
  },
  {
    term: "Sentinel wake-ups",
    cluster: "isolation",
    icon: "BellRing",
    commands: ["sentinel", "dispatch"],
    def: "A background sentinel polls every dispatched soldier's live status. It only interrupts you - via a Stop hook that blocks your turn from quietly ending - when one actually settles, done or blocked. No manual polling.",
    prompt:
      "I'll be heads-down on something else for a while, just let me know when the soldier on the auth refactor wraps up.",
  },
  {
    term: "Browser-capable soldiers",
    cluster: "isolation",
    icon: "Globe",
    commands: ["dispatch", "redispatch"],
    def: "A soldier can drive a real Chrome session through chrome-devtools-axi to click through a flow, not just read the code. If its pane disappears mid-session, redispatch tears down the orphaned browser bridge on its own.",
    prompt:
      "Have a soldier actually click through the signup flow in a browser and tell me if it's broken, not just read the code for it.",
  },
  {
    term: "Restart-proof state",
    cluster: "recovery",
    icon: "HardDrive",
    commands: ["doctor"],
    def: "Task state is JSON on disk, written atomically. herdr restores pane layout after a restart, but not process state - vexillum reconciles that itself from what's actually on disk, not from memory that's gone.",
    prompt:
      "My laptop crashed while a couple of soldiers were still running - what's actually still in progress?",
  },
  {
    term: "Redispatch, not resume",
    cluster: "recovery",
    icon: "RotateCw",
    commands: ["redispatch"],
    def: "Only an interrupted task can be redispatched. It relaunches the original prompt in a brand-new camp - the dead soldier's dirty worktree and any commits it never landed are discarded outright, nothing is resumed.",
    prompt: "The soldier that was in pane 3 got stuck, relaunch it.",
  },
  {
    term: "Camp lifecycle safety",
    cluster: "recovery",
    icon: "Lock",
    commands: ["release"],
    def: "release refuses outright if the camp's worktree has uncommitted changes, or if its branch isn't already landed on the base branch. A camp only goes back to the pool once there's nothing left to lose.",
    prompt: "Clean up after the soldier that just finished the logging fix.",
  },
  {
    term: "Land: local, or the real PR",
    cluster: "shipping",
    icon: "GitMerge",
    commands: ["land"],
    def: "land fast-forwards your checkout to the mission's branch - clean, local, no PR. For a mission that went through ship instead, it merges the real GitHub PR: open, not draft, mergeable, checks green via gh, matched to the exact head it just verified.",
    prompt: "The pagination fix looks good, land it.",
  },
  {
    term: "Ship through the no-mistakes gate",
    cluster: "shipping",
    icon: "ShieldCheck",
    commands: ["ship"],
    def: "ship pushes the mission's branch to a dedicated no-mistakes remote - self-configuring with no-mistakes init the first time. An isolated pipeline runs review, tests, lint, and docs, then opens the real PR itself once everything's green.",
    prompt:
      "This one touches the billing code, push it through the real gate - I want a proper reviewed PR, not a local merge.",
  },
  {
    term: "Environment doctor",
    cluster: "environment",
    icon: "Stethoscope",
    commands: ["doctor"],
    def: "doctor checks required tools (Claude Code, herdr) and optional ones tied to specific commands (no-mistakes, gh, tmux) - read-only, it verifies, never installs.",
    prompt:
      "Something feels off, can you check the environment's actually set up right before I dispatch anything else?",
  },
  {
    term: "AXI skills, on demand",
    cluster: "environment",
    icon: "Puzzle",
    commands: ["doctor"],
    def: "doctor also tracks a family of installable Agent Skills - quota-axi, lavish-axi, chrome-devtools-axi - and reports whether each is present, project-local or global. It never installs one for you; a soldier that needs one installs it itself, on demand.",
    prompt:
      "Can the next scout come back with an actual rendered mockup instead of just a wall of text?",
  },
  {
    term: "Your edits survive upgrades",
    cluster: "environment",
    icon: "FileLock2",
    commands: ["init", "upgrade"],
    def: ".claude/rules/vexillum.md is written once, by init, and left alone by every init or upgrade after that - so whatever you've tweaked in it by hand stays exactly as you left it.",
    prompt:
      "I tweaked the commander rules file last week - will upgrading vexillum wipe that out?",
  },
];

export const faqs = [
  {
    q: "Why Go?",
    a: "Distribution, not preference - a static Go binary installs by brew or curl with no runtime required on your machine.",
  },
  {
    q: "Why only Claude Code?",
    a: "Supporting multiple harnesses from day one would force a supervision abstraction before it's earned. A second harness is left as a seam, not built yet.",
  },
  {
    q: "Does vexillum open real pull requests?",
    a: "`vexillum land` merges locally by default. `vexillum ship` pushes a finished mission through a separate external validation gate that opens a real PR once it passes.",
  },
  {
    q: "What happens if a soldier crashes or the session restarts?",
    a: "Task state lives on disk. herdr restores the pane layout, but not process or task state - vexillum reconciles that from disk on its own.",
  },
];
