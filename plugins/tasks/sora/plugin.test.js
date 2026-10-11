import { describe, expect, test } from "bun:test";
import { buildContentRequest } from "./plugin.js";

const sourceUrl = "https://ark-acg-cn-beijing.tos-cn-beijing.volces.com/video/result.mp4?X-Tos-Signature=fixture%2Fsignature";
const context = {
  artifactKey: "video",
  baseUrl: "https://ai666.live",
  apiKey: "fixture-channel-key",
  upstreamTaskId: "fixture-upstream-task",
  clientRequest: { method: "GET" },
  data: { model: "seedance-2.5", metadata: { url: sourceUrl } },
};

describe("视频结果下载", () => {
  test("池畔 Seedance 完成结果直连源站且不泄露渠道密钥", () => {
    // 签名查询参数必须原样保留，直链请求不携带上游接口鉴权。
    expect(buildContentRequest(context)).toEqual({ url: sourceUrl, method: "GET", credentialless: true });
    expect(buildContentRequest({ ...context, clientRequest: { method: "HEAD" } })).toEqual({ url: sourceUrl, method: "HEAD", credentialless: true });
  });

  test.each([
    {},
    { model: "seedance-2.5", metadata: null },
    { model: "seedance-2.5", metadata: { url: "" } },
    { model: "seedance-2.5", metadata: { url: "https://other.example/result.mp4" } },
    { model: "seedance-2.5", metadata: { url: "https://ark-acg-cn-beijing.tos-cn-beijing.volces.com@other.example/result.mp4" } },
    { model: "seedance-2.5", metadata: { url: "http://ark-acg-cn-beijing.tos-cn-beijing.volces.com/result.mp4" } },
    { model: "seedance-2.5", metadata: { url: sourceUrl + "#fragment" } },
    { model: "seedance-2.0-933-per-second", metadata: { url: sourceUrl } },
  ])("没有目标模型有效直链时仍使用鉴权内容接口 %j", (data) => {
    expect(buildContentRequest({ ...context, data })).toEqual({
      url: "https://ai666.live/v1/videos/fixture-upstream-task/content",
      method: "GET",
      headers: { Authorization: "Bearer fixture-channel-key" },
    });
  });

  test("其他上游渠道不改变下载方式", () => {
    expect(buildContentRequest({ ...context, baseUrl: "https://other.example" })).toEqual({
      url: "https://other.example/v1/videos/fixture-upstream-task/content",
      method: "GET",
      headers: { Authorization: "Bearer fixture-channel-key" },
    });
  });
});
