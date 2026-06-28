"""
SILENT-INJECTION COGNITION — executable-shaped pseudocode (spec, not runnable)
===============================================================================
Maps 1:1 to the v5 architecture doc. Bodies are `...` where behavior is the
research question rather than the data structure.

LAYER NAMES: the layers are Subconscious / Conscious / Action (originally framed
as BACK / MIDDLE / FRONT in the v4 design; see
docs/reference/architecture-glossary.md §0). The live implementation is Go
(internal/...), not this Python sketch.

THREE OPERATION TYPES (the key trichotomy)
------------------------------------------
  1. INJECTION  (Subconscious)  silent · automatic · cheap     — you don't decide, it arrives
  2. THOUGHT-MCP (Conscious)    conscious · deliberate · internal — acts on the thought graph
  3. ACTION     (Action)        conscious · deliberate · effectful — acts on reality

  Two tool surfaces, symmetric:
    - external tools  -> Action     (change the world, watched seam)
    - the Thought MCP -> Conscious  (change your own reasoning) == "being able to think"

LAYERS                          SEAMS (opposite transparency)
  Subconscious = hidden engine   Subconscious<->Conscious = HIDDEN  (select + transform, unnoticed)
  Conscious    = main session    Conscious<->Action       = WATCHED (intention out, reality in)
  Action       = action layer
"""

from dataclasses import dataclass, field
from enum import Enum, auto

# ============================================================================
# SYMBOLIC THOUGHT TYPES
# ============================================================================

class Source(Enum):
    INJECTED    = auto()   # a Subconscious specialist fired, then re-voiced through the hidden seam
    GENERATED   = auto()   # Conscious's own serial effort (the "stuck / working it out" path)
    OBSERVATION = auto()   # reality feedback returned through Action (ground truth)
    USER_INPUT  = auto()   # unsolicited external input via the Interaction Port (interrupt)
    METACOG     = auto()   # produced by a Thought-MCP operation (branch/merge/rerank/...)

class Operator(Enum):      # abstract, DOMAIN-GENERAL transforms (not domain specialists)
    DECOMPOSE  = auto()
    VALIDATE   = auto()
    COMPARE    = auto()
    GENERALIZE = auto()
    ABSTRACT   = auto()
    SIMULATE   = auto()

class Resolution(Enum):
    EXPANDED   = auto()    # full detail — only the ACTIVE branch is allowed this
    COMPRESSED = auto()    # gist only — every stashed branch (bounded focus, lossy by design)

class Decision(Enum):      # what the Critic decides to do next
    THINK     = auto()     # keep going on the current branch
    BRANCH    = auto()     # fork (conflicting/divergent candidates)
    MERGE     = auto()     # two branches are the same / should combine
    BACKTRACK = auto()     # current branch exhausted -> pop to best stashed sibling
    ACT       = auto()     # closed loop exhausted -> open the channel to reality
    STOP      = auto()     # done

class Status(Enum):
    ACTIVE = auto(); STASHED = auto(); DEAD = auto(); MERGED = auto()


@dataclass
class Thought:
    id: int                       # the "thought count" — every thought is an ADDRESSABLE node
    text: str                     # first-person narrative content (post-transform if INJECTED)
    source: Source
    confidence: float = 0.0       # set by Critic.validate()
    operator: Operator | None = None
    parent: int | None = None     # graph edge to the thought it continued from
    raw_return: object | None = None   # pre-transform payload (INJECTED only) — kept for trace-back


@dataclass
class Branch:
    id: int
    thought_ids: list[int] = field(default_factory=list)
    resolution: Resolution = Resolution.EXPANDED
    summary: str | None = None    # the gist, populated when COMPRESSED
    value: float = 0.0            # rerank priority — DRIVEN BY THE VALUE SIGNAL (open problem)
    status: Status = Status.ACTIVE
    parent_branch: int | None = None


# ============================================================================
# THE THOUGHT GRAPH  (Conscious's substrate)
# ============================================================================

class ThoughtGraph:
    def __init__(self, goal: str):
        self.nodes: dict[int, Thought] = {}
        self.branches: dict[int, Branch] = {}
        self._tid = 0                    # the thought counter
        self._bid = 0
        self.active_branch: int = self._new_branch()
        self.append(Thought(self._next_id(), goal, Source.GENERATED))

    # ---- primitives -------------------------------------------------------
    def _next_id(self) -> int: self._tid += 1; return self._tid
    def _new_branch(self, parent=None) -> int:
        self._bid += 1
        self.branches[self._bid] = Branch(self._bid, parent_branch=parent)
        return self._bid

    def append(self, t: Thought) -> Thought:
        self.nodes[t.id] = t
        self.branches[self.active_branch].thought_ids.append(t.id)
        return t

    def active_context(self) -> list[Thought]:
        """The current branch at FULL resolution — what Subconscious reads and Conscious reasons over."""
        b = self.branches[self.active_branch]
        return [self.nodes[i] for i in b.thought_ids]

    def history(self) -> list[Thought]:
        """Re-entrant: Conscious can re-read and re-process its own prior thoughts."""
        return list(self.nodes.values())

    def generate(self, ctx) -> Thought:
        """Serial effortful loop — runs when NO specialist fired. This is felt-as-effort."""
        ...
        return Thought(self._next_id(), text="...", source=Source.GENERATED,
                       parent=ctx[-1].id if ctx else None)

    def form_intention(self) -> "Intention":
        """Distill the active branch into a single effectful action to take on the world."""
        ...


# ============================================================================
# THE THOUGHT MCP  — the "being able to think" interface
#   The MAIN MODEL deliberately calls these on its own graph (Source.METACOG).
#   The SAME operations also fire AUTOMATICALLY from the Gate (below).
#   Convertibility: repeated deliberate calls compile into automatic gate behavior.
# ============================================================================

class ThoughtMCP:
    """Metacognitive operations: deliberate, but NOT world-effectful."""
    def __init__(self, g: ThoughtGraph): self.g = g

    def branch(self, reason: str, seed: Thought | None = None) -> int:
        """Fork the context. Winner stays ACTIVE; the fork is a sibling to revisit."""
        new = self.g._new_branch(parent=self.g.active_branch)
        ...
        return new

    def merge(self, a: int, b: int) -> int:
        """Two branches are the same point / should combine into one line of thought."""
        ...

    def rerank(self) -> list[int]:
        """Reprioritize the frontier: return branches ordered by `value` (the value signal)."""
        return sorted(self.g.branches, key=lambda i: self.g.branches[i].value, reverse=True)

    def compress(self, branch_id: int) -> None:
        """Fade an inactive branch to gist (frees focus). LOSSY BY DESIGN."""
        b = self.g.branches[branch_id]
        b.summary = summarize([self.g.nodes[i] for i in b.thought_ids])   # ...
        b.resolution = Resolution.COMPRESSED

    def expand(self, branch_id: int) -> None:
        """Restore a stashed branch to full detail, or unfold one thought into sub-thoughts."""
        ...
        self.g.branches[branch_id].resolution = Resolution.EXPANDED

    def focus(self, branch_id: int) -> None:
        """Switch active branch: compress the one we leave, expand the one we enter."""
        self.compress(self.g.active_branch)
        self.expand(branch_id)
        self.g.active_branch = branch_id


# ============================================================================
# SUBCONSCIOUS — the hidden engine (silent specialists + dispatch + workflow/operators)
# ============================================================================

class Specialist:
    """A silent sub-agent bound to one domain (memory, arithmetic, simulation, ...)."""
    def __init__(self, domain: str): self.domain = domain
    def relevance(self, ctx) -> float: ...      # how strongly this domain lights up
    def fire(self, ctx) -> object: ...          # runs SILENTLY, returns a raw payload

@dataclass
class Phase:
    operator: Operator
    subagent: Specialist | None = None          # instantiated at runtime

class Workflow:
    """Learned operator sequence (e.g. DECOMPOSE -> GENERATE -> VALIDATE). Convertible."""
    def __init__(self, phases: list[Phase]): self.phases = phases; self.i = 0
    def recognize(self, ctx) -> bool: ...        # "this is a design-build-validate shape"
    def current(self) -> Phase: return self.phases[self.i]
    def instantiate(self, phase: Phase) -> Specialist: ...   # ephemeral, runtime-defined
    def gate_bias(self) -> dict: ...             # privilege certain specialists this phase
    def advance(self): self.i = min(self.i + 1, len(self.phases) - 1)

class SubconsciousEngine:
    def __init__(self, specialists: list[Specialist]):
        self.specialists = specialists
        self.workflow: Workflow | None = None

    def dispatch(self, ctx) -> list[object]:
        """PULL, not push: every specialist reads ctx and fires IF its domain is relevant.
           No orchestrator chooses them. A recognized workflow may bias/sequence firing."""
        if self.workflow and self.workflow.recognize(ctx):
            phase = self.workflow.current()
            phase.subagent = phase.subagent or self.workflow.instantiate(phase)
        return [s.fire(ctx) for s in self.specialists if s.relevance(ctx) > RELEVANCE_THRESHOLD]


# ============================================================================
# THE GATE + THE CRITIC  (where the cognition actually lives)
# ============================================================================

class Gate:
    """Arbitrates competing candidates. FORKS rather than discards:
       winner -> active focus; losers -> retained, COMPRESSED sibling branches.
       Runs AFTER the Filter, on already-admitted survivors."""
    def select(self, candidates: list[object], history, bias: dict) -> object: ...
    def conflicting(self, candidates: list[object]) -> bool: ...   # disagreement -> auto-branch

class Filter:
    """The Critic, half 1 — ADMISSION. Validates a RAW candidate BEFORE transform voices it.
       Sits on the intake (the hidden seam for injections; also screens generated,
       observation, and user input). validate-before-voice kills laundered hallucination."""
    def admit(self, raw_candidate: object, history, value: float) -> tuple[bool, float]:
        ...  # -> (admit?, confidence). REJECT / FLAG / ADMIT.

class Controller:
    """The Critic, half 2 — EXECUTIVE. Reasons over the WHOLE GRAPH, lives in Conscious.
       Exhaustion, decide-next, stopping, quiescence, and interrupt handling."""
    def branch_exhausted(self, g: "ThoughtGraph") -> bool: ...   # current line spent
    def loop_exhausted(self, g: "ThoughtGraph") -> bool: ...     # ALL internal options spent -> ACT
    def decide_next(self, g: "ThoughtGraph", candidates) -> "Decision": ...
    def quiescent(self, g, pending_input: bool, outstanding_action: bool) -> bool:
        """IDLE condition: goal resolved/abandoned AND no input AND no action AND
           no stashed branch above the unprompted-pursuit threshold."""
        ...
    def on_interrupt(self, g, user_thought: "Thought") -> None:
        """Compress active branch (SUSPEND), focus a branch for the input, re-seed value."""
        ...


# ============================================================================
# THE TWO SEAMS
# ============================================================================

class HiddenSeam:
    """Subconscious -> Conscious. INVISIBLE BY DESIGN. filter + select + transform.
       Pipeline: FILTER (admit raw) -> GATE (arbitrate survivors) -> TRANSFORM (re-voice)."""
    def __init__(self, gate: Gate, filt: Filter):
        self.gate = gate; self.filter = filt

    def relay(self, raw_returns, history, bias, value) -> "Thought | None":
        survivors = [r for r in raw_returns if self.filter.admit(r, history, value)[0]]  # FILTER (raw)
        if not survivors:
            return None                                                                  # nothing admitted
        winner = self.gate.select(survivors, history, bias)                              # SELECT
        text   = transform_to_narrative(winner, history)                                 # TRANSFORM
        return Thought(id=-1, text=text, source=Source.INJECTED, raw_return=winner)
        # ^ validated BEFORE voicing; lands as Conscious's own next thought; seam not perceived.


class InteractionPort:
    """External inbound. Delivers USER_INPUT as a high-salience thought and can INTERRUPT.
       Symmetry: hearing = USER_INPUT (here); speaking = an Action (watched seam)."""
    def __init__(self): self.inbox: list[object] = []
    def pending(self) -> bool: return bool(self.inbox)
    def receive(self, message) -> None: self.inbox.append(message)        # async; may interrupt
    def deliver(self, filt: Filter, history, value) -> "Thought | None":
        if not self.inbox:
            return None
        msg = self.inbox.pop(0)
        ok, conf = filt.admit(msg, history, value)                        # parsed / screened
        return Thought(-1, str(msg), Source.USER_INPUT, confidence=conf) if ok else None

class WatchedSeam:
    """Conscious -> Action. VISIBLE BY DESIGN. intention out, reality in."""
    def __init__(self, actuator: "ActionActuator"): self.actuator = actuator

    def open_to_reality(self, intention) -> Thought:
        obs = self.actuator.act(intention)                            # CONSCIOUS, monitored
        return Thought(id=-1, text=describe(obs), source=Source.OBSERVATION, raw_return=obs)


class ActionActuator:
    """The opening. Effectful, reality-facing. Its PURPOSE is to import ground truth
       the closed loop cannot manufacture (run the code, the experiment, send the thing)."""
    def act(self, intention) -> object:
        result = execute_in_world(intention)    # IDE / bash / send / measure ...
        return result                            # must be CAUGHT in the open or it isn't feedback


# ============================================================================
# THE MAIN LOOP  (the algorithm)
# ============================================================================

# ============================================================================
# THE LIFECYCLE + MAIN LOOP  (the algorithm, wrapped in a system state machine)
# ============================================================================

class SystemState(Enum):
    IDLE             = auto()   # no goal; background consolidation may run
    ACTIVE           = auto()   # thinking loop running
    AWAITING_REALITY = auto()   # suspended on an Action's feedback
    AWAITING_USER    = auto()   # turn handed back to the user
    SUSPENDED        = auto()   # paused mid-task (interrupt / budget cap), resumable
    DONE             = auto()   # episode terminal -> IDLE


def run_system():
    g       = None
    subc    = SubconsciousEngine(specialists=[...])
    gate, filt, ctrl = Gate(), Filter(), Controller()
    hidden  = HiddenSeam(gate, filt)
    watched = WatchedSeam(ActionActuator())
    port    = InteractionPort()
    mcp     = None
    state   = SystemState.IDLE

    while True:
        # ---- IDLE: nothing in the foreground -> consolidate in the background ----
        if state is SystemState.IDLE:
            background_consolidate()                      # convertibility compiles (rest/sleep)
            if port.pending():                            # USER_INPUT wakes the system
                g = ThoughtGraph(goal="(from user)"); mcp = ThoughtMCP(g)
                state = SystemState.ACTIVE
            else:
                continue                                  # stay quiescent

        # ---- ACTIVE: one step of the thinking loop ----
        # 0. INTERRUPT: unsolicited USER_INPUT preempts the current branch.
        if port.pending():
            u = port.deliver(filt, g.history(), value_of(g))
            if u:
                u.id = g._next_id(); g.append(u)
                ctrl.on_interrupt(g, u)                    # compress active branch, refocus, re-seed value

        ctx  = g.active_context()
        bias = subc.workflow.gate_bias() if subc.workflow else {}

        # 1. SUBCONSCIOUS fires silently, in parallel (PULL).
        raw = subc.dispatch(ctx)

        # 2. INTAKE PIPELINE: FILTER (admit raw) -> GATE (arbitrate) -> TRANSFORM (re-voice).
        #    If nothing is admitted/fired, Conscious pays the effortful serial cost itself.
        t = hidden.relay(raw, g.history(), bias, value_of(g)) or g.generate(ctx)

        # 2a. Conflicting admitted returns auto-fork (Gate calls the SAME op the MCP exposes).
        if raw and gate.conflicting(raw):
            mcp.branch(reason="conflicting injections")

        # 3. Append. GENERATED thoughts are filtered here (they don't cross the hidden seam).
        t.id = g._next_id()
        if t.source is Source.GENERATED:
            _, t.confidence = filt.admit(t.text, g.history(), value_of(g))
        g.append(t)

        # 4. CONTROLLER decides the next move over the whole graph.
        match ctrl.decide_next(g, raw):
            case Decision.THINK:
                pass
            case Decision.BRANCH:
                mcp.branch(reason="deliberate: explore alternative")        # METACOG
            case Decision.MERGE:
                ranked = mcp.rerank(); mcp.merge(ranked[0], ranked[1])
            case Decision.BACKTRACK:                                        # branch spent, loop not
                mcp.focus(mcp.rerank()[0])                                  # pop to best stashed sibling
            case Decision.ACT:                                              # loop exhausted -> reality
                state = SystemState.AWAITING_REALITY
                obs = watched.open_to_reality(g.form_intention())           # WATCHED seam (monitored)
                obs.id = g._next_id(); g.append(obs)
                state = SystemState.ACTIVE
            case Decision.STOP:
                respond_to_user_via_action(g, watched)                      # speaking = an Action
                state = (SystemState.AWAITING_USER if conversational(g) else SystemState.DONE)

        if subc.workflow:
            subc.workflow.advance()

        # ---- transitions out of the step ----
        if state is SystemState.DONE:
            if ctrl.quiescent(g, port.pending(), outstanding_action=False):
                state = SystemState.IDLE                                    # reach IDLE (quiescence)
        elif state is SystemState.AWAITING_USER and port.pending():
            state = SystemState.ACTIVE                                      # next turn

# NOTE: run_system() above is the REACTIVE (episodic) regime — it waits to be prompted
# and ACT blocks. It is the SPECIAL CASE of the continuous regime below, obtained by
# turning drives/default-mode off and coupling arousal to input arrival.


# ============================================================================
# CONTINUOUS (AWAKE) MODE  — the general regime: self-sustaining perceive·think·act
# ============================================================================

class Arousal(Enum):
    AWAKE  = auto()    # fast continuous loop, full perception gain
    DROWSY = auto()    # slowed loop, dampened perception, more spontaneous
    ASLEEP = auto()    # foreground halted; consolidation dominates ("dreaming")

class Drives:
    """Endogenous goals when none are given: curiosity, unfinished high-value branches,
       maintenance. Feeds the value signal so the loop always has a gradient to follow."""
    def propose_goal(self, g, value) -> "Thought | None": ...   # None if SATED

class DefaultMode:
    """Spontaneous associative firing when no task/drive dominates (mind-wandering).
       A spontaneous thought can clear the unprompted-pursuit threshold and BECOME a goal."""
    def wander(self, internal_state) -> list[object]: ...

class PerceptionPort(InteractionPort):
    """Generalizes the Interaction Port: an ALWAYS-ON afferent stream. Percepts (including
       USER_INPUT and async action-feedback) arrive unsolicited and compete for attention."""
    def stream(self, gain: float) -> list[object]: ...          # gain scales with arousal

class AsyncWatchedSeam(WatchedSeam):
    """Non-blocking: fire an action and KEEP THINKING; feedback returns later as a percept."""
    def fire(self, intention) -> None: ...                      # dispatch, do not wait
    # feedback arrives later via PerceptionPort as Source.OBSERVATION


def run_continuous():
    g       = ThoughtGraph(goal="(idle/awake)"); mcp = ThoughtMCP(g)
    subc    = SubconsciousEngine(specialists=[...])
    gate, filt, ctrl = Gate(), Filter(), Controller()
    hidden  = HiddenSeam(gate, filt)
    awatched = AsyncWatchedSeam(ActionActuator())
    percept = PerceptionPort()
    drives, dmn = Drives(), DefaultMode()
    arousal = Arousal.AWAKE

    while True:                                          # SELF-SUSTAINING: no wait-for-prompt
        # ---- arousal gates the regime ----
        if arousal is Arousal.ASLEEP:
            background_consolidate()                     # dreaming = replay compressed branches
            if percept.salient():                        # a salient percept wakes the system
                arousal = Arousal.AWAKE
            continue

        gain = perception_gain(arousal)

        # 1. CONTINUOUS PERCEPTION (always on): percepts + async action-feedback stream in.
        for p in percept.stream(gain):
            pt = port_to_thought(p, filt, g.history(), value_of(g))   # screened by Filter
            if pt:
                pt.id = g._next_id(); g.append(pt)
                if is_interrupt(p): ctrl.on_interrupt(g, pt)

        # 2. WHAT TO THINK ABOUT: task -> drive -> mind-wandering (in priority order).
        if ctrl.no_active_goal(g):
            goal = drives.propose_goal(g, value_of(g))                # endogenous goal
            if goal is None:
                raw = dmn.wander(g.internal_state())                  # default mode
            else:
                g.append(goal); raw = subc.dispatch(g.active_context())
        else:
            raw = subc.dispatch(g.active_context())                   # task-driven

        # 3. INTAKE: FILTER -> GATE -> TRANSFORM (or effortful generation).
        bias = subc.workflow.gate_bias() if subc.workflow else {}
        t = hidden.relay(raw, g.history(), bias, value_of(g)) or g.generate(g.active_context())
        t.id = g._next_id(); g.append(t)

        # 4. CONTROLLER decides — ACT is NON-BLOCKING here (think continues next tick).
        match ctrl.decide_next(g, raw):
            case Decision.BRANCH:    mcp.branch("explore alternative")
            case Decision.MERGE:     r = mcp.rerank(); mcp.merge(r[0], r[1])
            case Decision.BACKTRACK: mcp.focus(mcp.rerank()[0])
            case Decision.ACT:       awatched.fire(g.form_intention())  # FIRE & CONTINUE (async)
            case Decision.STOP:      pass                                # in awake mode, no hard stop
            case _:                  pass

        # 5. arousal regulation (the cost of being awake: must self-regulate or thrash).
        arousal = regulate_arousal(arousal, g, percept, drives)         # may drop to DROWSY/ASLEEP


# ============================================================================
# CONVERTIBILITY  (learning to think cheaper — runs across episodes / during sleep)
# ============================================================================

def compile_with_practice(traces):
    """Migrate effortful -> automatic. The single mechanism behind 'getting smart':
         - repeated GENERATED pattern   -> new Specialist        (fact/compute becomes injected)
         - repeated METACOG sequence    -> Workflow / gate habit (control flow becomes automatic)
       Current LLM harnesses can't do this at inference; weights are frozen."""
    ...


# ============================================================================
# THE VALUE SIGNAL & TRAINING  (the heuristic that makes the durable search converge)
#   One scalar V(state) consumed at four sites:
#     - Branch.value           : which branch deserves expansion?           (rerank)
#     - Filter.admit           : trust this raw candidate before voicing?   (intake)
#     - Controller.loop_exhausted : think more, or pay to ACT?              (spine)
#     - compile_with_practice  : which patterns are worth compiling?        (convertibility)
#
#   RL formulation:  state = thought-graph context; action = a Thought-MCP op;
#                    reward = reality-confirmed outcome (an OBSERVATION) — GROUNDED, never self-graded;
#                    policy = Gate; value = rerank heuristic; credit = TD over the graph.
# ============================================================================

def value(state) -> float:
    """Bootstrap: LLM-propose-K + recommend (approximate V, zero training).
       Then: distill (recommender choices + GROUNDED reward) into a cheap learned V.
       Judgment migrates expensive->cheap, exactly like skills migrate generated->injected."""
    ...

def reward(graph) -> float:
    """MUST come from reality-confirmed OBSERVATIONs, not from a model grading its own traces.
       Self-grading distills confidence (incl. confident mistakes). Reality calibrates."""
    ...

# DESIGN OPTION (tagged, not baked in): asymmetric model sizes.
#   tiny fast model runs Conscious; exposed PARAMETERLESS tools (intent only);
#   a bigger model resolves parameters at fire-time (the Transform/effector seam-crossing).
#   Bet: if Conscious mostly THINKS rather than calls tools, the Subconscious does the heavy
#   lifting and the hot loop stays cheap -> higher sustainable rate λ̄ = μ/(1-n) per unit compute.
#   Guard: Filter/value must gate INTENT before the big model spends on resolution.

# STAGED COMPRESSION: (1) all-frontier scaffold harvests traces ->
#   (2) RL/distill the convert-first tier (value/rerank, specialists, Filter, Transform) ->
#   (3) small Conscious + distilled Subconscious + learned V; frontier kept only at hard seam-crossings.
#   Discipline: grounded reward always; train Filter + value BEFORE shrinking Conscious.
# ============================================================================
