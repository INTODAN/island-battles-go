// Opens a demo file in the viewer. The page's own script, the replay's,
// draws it once readDemo has checked it. The file is read in the browser
// and never uploaded.
import { DemoFileError, describeDemo, fileSizeProblem, readDemo } from "./demo-file.mjs";

const bar = document.querySelector(".demo-file");
const expected = { format: bar.dataset.format, schemaVersion: Number(bar.dataset.schemaVersion) };
const input = document.getElementById("demo-input");
const status = document.getElementById("demo-status");
const problem = document.getElementById("demo-error");

function showProblem(message) {
  problem.textContent = message;
  problem.hidden = !message;
}

document.getElementById("open-demo").addEventListener("click", () => input.click());
input.addEventListener("change", async () => {
  const file = input.files[0];
  // Choosing the same file again opens it again.
  input.value = "";
  if (!file) return;
  showProblem("");
  // A demo that is already open stays open when another file can't be.
  const keeps = document.getElementById("view").hidden ? "" : " The demo shown before stays open.";
  const sizeProblem = fileSizeProblem(file.size);
  if (sizeProblem) {
    showProblem(sizeProblem + keeps);
    return;
  }
  let demo;
  try {
    demo = readDemo(await file.text(), expected);
  } catch (error) {
    showProblem(`${error instanceof DemoFileError ? error.message : "The file could not be read."}${keeps}`);
    return;
  }
  window.showDemo(demo);
  status.textContent = `Opened ${file.name}.`;
  document.getElementById("demo-summary").textContent = describeDemo(demo);
});
