import { describe, expect, it } from "vitest";
import { createChatOperationKey, rotateChatOperationKey } from "./chatOperations";

describe("chat operation identities", () => {
  it("reuses uncertain operation identities until success rotates them", () => {
    const first = createChatOperationKey("video", "42:7");
    const retry = createChatOperationKey("video", "42:7");
    expect(retry).toBe(first);
    rotateChatOperationKey("video", "42:7");
    expect(createChatOperationKey("video", "42:7")).not.toBe(first);
  });
});
