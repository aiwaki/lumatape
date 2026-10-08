import test from "node:test";
import assert from "node:assert/strict";
import { bindEmergencyControls } from "../actions.mjs";

test("footer and modal STOP close the dialog and call the same emergency without confirming its action", () => {
  const footer = new EventTarget(), modal = new EventTarget();
  const operations = [];
  const dialog = { open: false, close() { this.open = false; operations.push("close"); } };
  bindEmergencyControls([footer, modal], dialog, () => {
    assert.equal(dialog.open, false);
    operations.push("emergency");
  });
  footer.dispatchEvent(new Event("click"));
  assert.deepEqual(operations, ["emergency"]);
  // Simulate either Quit or Update confirmation. STOP must not activate it.
  dialog.open = true;
  modal.dispatchEvent(new Event("click"));
  assert.deepEqual(operations, ["emergency", "close", "emergency"]);
});
