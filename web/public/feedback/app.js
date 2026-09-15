const status = document.querySelector("#status");
const container = document.querySelector("#items");
let answerRevision = "";
let answers = {},
  timer,
  queue = Promise.resolve(),
  revision = 0;
function save() {
  status.textContent = "Saving…";
  const body = JSON.stringify(answers),
    current = ++revision;
  queue = queue
    .catch(() => {})
    .then(async () => {
      try {
        const r = await fetch("/feedback/api/answers", {
          method: "POST",
          headers: {
            "Content-Type": "application/json",
            "If-Match": answerRevision,
          },
          body,
        });
        if (!r.ok)
          throw new Error(
            r.status === 401
              ? "Session expired. Sign in again before saving."
              : "Could not save — keep this page open and try again.",
          );
        answerRevision = r.headers.get("etag");
        if (current === revision) status.textContent = "All answers saved";
      } catch (e) {
        status.textContent = e.message;
      }
    });
}
fetch("/feedback/api/items")
  .then(async (r) => {
    if (!r.ok) throw new Error("Unable to load records. Please sign in again.");
    answerRevision = r.headers.get("etag");
    return r.json();
  })
  .then((data) => {
    answers = data.answers;
    const items = data.items.sort((a, b) =>
      a.transaction_date.localeCompare(b.transaction_date),
    );
    status.textContent = items.length
      ? `${items.length} records to review`
      : "No records need review right now. Your saved notes are retained.";
    for (const item of items) {
      const card = document.createElement("section");
      card.className = "card";
      const top = document.createElement("div");
      top.className = "top";
      const title = document.createElement("h2");
      title.textContent = item.original_name;
      const amount = document.createElement("div");
      amount.className = "amount";
      amount.textContent =
        (item.amount_minor < 0 ? "+" : "−") +
        new Intl.NumberFormat("en-US", {
          style: "currency",
          currency: item.currency || "USD",
        }).format(Math.abs(item.amount_minor) / 100);
      top.append(title, amount);
      const meta = document.createElement("div");
      meta.className = "meta";
      meta.textContent = `${item.transaction_date} · ${item.account_name}`;
      const input = document.createElement("textarea");
      input.value = answers[item.source_uid] || "";
      input.placeholder = "What was this for?";
      input.setAttribute("aria-label", `${item.original_name} explanation`);
      function changed() {
        answers[item.source_uid] = input.value;
        card.classList.toggle("done", Boolean(input.value.trim()));
        clearTimeout(timer);
        timer = setTimeout(save, 350);
      }
      input.addEventListener("input", changed);
      input.addEventListener("blur", () => {
        clearTimeout(timer);
        save();
      });
      const actions = document.createElement("div");
      actions.className = "actions";
      for (const label of ["Personal spending", "Refund", "I don’t remember"]) {
        const button = document.createElement("button");
        button.textContent = label;
        button.onclick = () => {
          input.value = label;
          changed();
        };
        actions.append(button);
      }
      card.append(top, meta, input, actions);
      container.append(card);
    }
  })
  .catch((e) => {
    status.textContent = e.message;
  });
