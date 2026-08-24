import assert from "node:assert/strict";
import test from "node:test";

import { modalState, resolveModal, showModal } from "./useModal.js";

test("showModal cancels the previous request when a modal is replaced", async () => {
  const first = showModal({ content: "first" });
  const second = showModal({ content: "second" });

  assert.equal(await first, false);
  assert.equal(modalState.visible, true);
  assert.equal(modalState.content, "second");

  resolveModal(true);
  assert.equal(await second, true);
  assert.equal(modalState.visible, false);
  assert.equal(modalState._resolve, null);
});