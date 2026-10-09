const RATIOS = ["auto", "1:1", "3:2", "2:3", "4:3", "3:4", "5:4", "4:5", "16:9", "9:16", "21:9", "9:21"];

export const meta = {
  apiVersion: 1,
  key: "midjourney",
  name: "Midjourney",
  description: { en: "Midjourney image generation", zh: "Midjourney 图片生成" },
  version: "1.0.0",
  author: { name: "QuantumNous" },
  channelTypes: [2, 5],
  models: ["mj-v8.2", "mj-niji7"],
  fetchMode: "per_task",
  routes: [{ method: "POST", path: "/v1/midjourney/generations", type: "submit", action: "IMAGE", decode: "decodeSubmit", render: "taskCreated" }],
};

function validateRequest(body) {
  if (!body || typeof body !== "object" || Array.isArray(body)) throw new Error("request body must be an object");
  if (typeof body.model !== "string" || !body.model.trim()) throw new Error("model is required");
  if (typeof body.prompt !== "string" || !body.prompt.trim()) throw new Error("prompt is required");
  // 四张单图属于一次生成，禁止把返回张数当成提交次数参与计费。
  if (body.n !== undefined && body.n !== 1) throw new Error("n must be 1");
  if (body.size !== undefined && !RATIOS.includes(body.size)) throw new Error("size must be an aspect ratio");
  for (const key of ["raw", "tile"]) {
    if (body[key] !== undefined && typeof body[key] !== "boolean") throw new Error(key + " must be a boolean");
  }
  if (body.quality !== undefined && ![0.25, 0.5, 1, 2].includes(body.quality)) throw new Error("invalid quality");
  const bounds = { stylize: 1000, chaos: 100, weird: 3000, iw: 3, cw: 100, sw: 1000, dw: 100 };
  for (const key of Object.keys(bounds)) {
    if (body[key] !== undefined && (typeof body[key] !== "number" || !Number.isFinite(body[key]) || body[key] < 0 || body[key] > bounds[key])) throw new Error("invalid " + key);
  }
  if (body.seed !== undefined && !Number.isSafeInteger(body.seed)) throw new Error("seed must be an integer");
  if (body.negative_prompt !== undefined && typeof body.negative_prompt !== "string") throw new Error("negative_prompt must be a string");
  if (body.images !== undefined && (!Array.isArray(body.images) || body.images.length < 1 || body.images.length > 5)) throw new Error("images must contain 1 to 5 references");
  if (body.image !== undefined && body.images !== undefined) throw new Error("use image or images, not both");
  for (const image of [body.image, body.cref, body.sref, body.dref].concat(body.images || [])) {
    if (image !== undefined && (typeof image !== "string" || !/^(https?:\/\/\S+|data:image\/[^;,]+;base64,\S+)$/i.test(image))) throw new Error("invalid image reference");
  }
  // 后续动作需要解析父任务并绑定渠道，不能直接转发客户端提供的私有编号。
  if (body.action !== undefined || body.task_id !== undefined) throw new Error("this route accepts image generation only");
}

export const native = {
  decodeSubmit(ctx) {
    if (!ctx.body || ctx.body.kind !== "json") throw new Error("JSON body required");
    validateRequest(ctx.body.value);
    return { kind: "submit", model: ctx.body.value.model, requestBody: ctx.body.value };
  },
  taskCreated(ctx, task) {
    return { data: { task_id: task.task_id, status: "submitted", progress: "0%" } };
  },
};

export function buildSubmitRequest(ctx) {
  validateRequest(ctx.requestBody);
  // 只替换已经由渠道配置解析出的上游模型名，不改提示词或参考图顺序。
  return {
    url: ctx.baseUrl.replace(/\/$/, "") + "/v1/midjourney/generations",
    method: "POST",
    headers: { Authorization: "Bearer " + ctx.apiKey, "Content-Type": "application/json" },
    body: Object.assign({}, ctx.requestBody, { model: ctx.upstreamModel || ctx.model }),
  };
}

export function parseSubmitResponse(ctx, resp) {
  const body = resp.body || {}, data = body.data || {};
  if (typeof data.task_id !== "string" || !data.task_id) throw new Error(data.error_message || body.message || "missing task_id");
  return { taskId: data.task_id, taskData: body };
}

// 使用现有按次价格，返回四张图不增加倍率；失败退款由宿主结算链处理。
export function extractUsage() { return {}; }
export function extractUsageOnComplete() { return {}; }

export function buildQueryRequest(ctx) {
  return { url: ctx.baseUrl.replace(/\/$/, "") + "/v1/tasks/" + encodeURIComponent(ctx.taskId), method: "GET", headers: { Authorization: "Bearer " + ctx.apiKey } };
}

export function parseTaskResult(ctx, body) {
  const data = body.data || {};
  const statuses = { submitted: "SUBMITTED", queued: "QUEUED", pending: "QUEUED", processing: "IN_PROGRESS", in_progress: "IN_PROGRESS", completed: "SUCCESS", failed: "FAILURE", cancelled: "FAILURE", canceled: "FAILURE" };
  const status = statuses[data.status];
  if (!status) return { status: "UNKNOWN", reason: "unknown task status: " + String(data.status || "") };
  return { status, progress: typeof data.progress === "string" ? data.progress : "", reason: status === "FAILURE" ? data.error_message || "task failed" : "" };
}
