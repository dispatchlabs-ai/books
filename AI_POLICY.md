# AI-assisted contribution policy

AI tools may assist with Books, but they do not own issues, changes, or releases.
A named human remains accountable.

Every AI-assisted pull request must disclose:

- the tool or model used;
- which parts of the work it materially influenced;
- what the human independently reviewed; and
- the verification performed outside the model's assertions.

The human submitter must understand the code, tests, accounting effect, data
safety, and failure modes. They must be able to maintain the change without the
original conversation. Raw model output, unattended issue generation,
unreviewed bulk changes, and submissions that merely report “the agent says the
tests pass” are not acceptable.

Agents must follow `AGENTS.md`, use disposable data, operate without repository
or production secrets, and obtain explicit human authority before creating or
changing external GitHub state.
