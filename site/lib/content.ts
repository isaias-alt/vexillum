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
