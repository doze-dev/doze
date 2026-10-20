// For the data-plane operations an app actually calls, which input member names the resource?
const models = "/Users/srini/Code/doze-dev/doze-aws/.audit-models/";
const want: Record<string, string[]> = {
  "sqs.json": ["SendMessage", "SendMessageBatch", "ReceiveMessage", "DeleteMessage", "GetQueueUrl"],
  "sns.json": ["Publish", "PublishBatch", "Subscribe"],
  "s3.json": ["PutObject", "GetObject", "DeleteObject", "ListObjectsV2", "HeadObject", "CopyObject"],
  "dynamodb.json": ["PutItem", "GetItem", "Query", "Scan", "UpdateItem", "BatchWriteItem", "TransactWriteItems"],
  "kinesis.json": ["PutRecord", "PutRecords", "GetRecords", "GetShardIterator"],
  "lambda.json": ["Invoke"],
  "eventbridge.json": ["PutEvents"],
  "sfn.json": ["StartExecution", "StartSyncExecution"],
  "secrets-manager.json": ["GetSecretValue"],
  "ssm.json": ["GetParameter", "GetParameters", "GetParametersByPath"],
};
for (const [f, ops] of Object.entries(want)) {
  const m = JSON.parse(await Bun.file(models + f).text());
  const sh = m.shapes;
  for (const op of ops) {
    const id = Object.keys(sh).find((k) => k.endsWith("#" + op) && sh[k].type === "operation");
    if (!id) { console.log(f, op, "MISSING"); continue; }
    const inp = sh[sh[id].input.target];
    const mem = Object.entries<any>(inp.members ?? {});
    const req = mem.filter(([, v]) => v.traits?.["smithy.api#required"] !== undefined).map(([k]) => k);
    const ctx = mem.filter(([, v]) => v.traits?.["smithy.rules#contextParam"]).map(([k]) => k);
    const ocp = sh[id].traits?.["smithy.rules#operationContextParams"];
    console.log(`${op.padEnd(22)} required=[${req.join(",")}] contextParam=[${ctx.join(",")}]${ocp ? " opContext=" + JSON.stringify(ocp) : ""}`);
  }
}
