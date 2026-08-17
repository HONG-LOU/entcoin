import test from "node:test";
import assert from "node:assert/strict";

globalThis.window = {
  localStorage: { getItem: () => "en", setItem: () => {} },
};

const { canDeleteEntPaySession, entpayPageCount, entpayStageLabel, formatEnt, normalizeEntPayResult, parseEnt, paymentSent, revisionMatches } = await import("../src/entpay.js");

test("formats atomic EntPay amounts exactly without floating point", () => {
  assert.equal(formatEnt(1), "0.00000001 ENT");
  assert.equal(formatEnt(123456789), "1.23456789 ENT");
  assert.equal(formatEnt("900719925474099300"), "9,007,199,254.740993 ENT");
});

test("parses settings amounts without floating point rounding", () => {
  assert.equal(parseEnt("0.01000000"), 1000000);
  assert.equal(parseEnt("1.23456789"), 123456789);
  assert.throws(() => parseEnt("0.000000001"));
  assert.throws(() => parseEnt("0"));
});

test("maps every user-facing terminal and approval stage", () => {
  assert.equal(entpayStageLabel("awaiting_approval"), "Awaiting approval");
  assert.equal(entpayStageLabel("failed_retryable"), "Action required");
  assert.equal(entpayStageLabel("failed_terminal"), "Verification failed");
});

test("revision guards reject stale or malformed actions", () => {
  assert.equal(revisionMatches("7", 7), true);
  assert.equal(revisionMatches("6", 7), false);
  assert.equal(revisionMatches("not-a-number", 7), false);
});

test("transaction presence keeps post-broadcast failures labelled as paid", () => {
  assert.equal(paymentSent({ stage: "failed_retryable", transaction_id: "tx1" }), true);
  assert.equal(paymentSent({ stage: "failed_terminal", transaction_id: "tx1" }), true);
  assert.equal(paymentSent({ stage: "awaiting_approval" }), false);
});

test("generic EntPay results require a schema, summary and opaque data", () => {
  assert.deepEqual(
    normalizeEntPayResult({ schema: "entpay-result-v1", summary: "Service completed", data: { value: 42 } }),
    { schema: "entpay-result-v1", summary: "Service completed", data: { value: 42 } },
  );
  assert.equal(normalizeEntPayResult({ summary: "Missing schema", data: {} }), null);
  assert.equal(normalizeEntPayResult({ schema: "entpay-result-v1", summary: "", data: {} }), null);
});

test("paginates payment requests in compact five-item pages", () => {
  assert.equal(entpayPageCount(0), 1);
  assert.equal(entpayPageCount(5), 1);
  assert.equal(entpayPageCount(6), 2);
  assert.equal(entpayPageCount(200), 40);
});

test("only exposes deletion when request tracking can be safely removed", () => {
  assert.equal(canDeleteEntPaySession({ stage: "awaiting_approval" }), true);
  assert.equal(canDeleteEntPaySession({ stage: "failed_retryable" }), true);
  assert.equal(canDeleteEntPaySession({ stage: "failed_retryable", transaction_id: "tx1" }), false);
  assert.equal(canDeleteEntPaySession({ stage: "confirming", transaction_id: "tx1" }), false);
  assert.equal(canDeleteEntPaySession({ stage: "complete", transaction_id: "tx1" }), true);
});
