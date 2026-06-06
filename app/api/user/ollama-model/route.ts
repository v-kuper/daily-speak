import { NextResponse, type NextRequest } from "next/server";
import { SESSION_COOKIE_NAME, getUserBySessionToken } from "../../../../src/server/auth";
import {
  getDefaultOllamaIsThinkingModel,
  getDefaultOllamaModel
} from "../../../../src/server/ollama";
import { createRouteLogger, elapsedMs, toErrorMeta } from "../../../../src/server/logger";

export const runtime = "nodejs";
export const dynamic = "force-dynamic";

const resolveAuthorizedUser = async (request: NextRequest) => {
  const token = request.cookies.get(SESSION_COOKIE_NAME)?.value;
  return getUserBySessionToken(token);
};

const buildModelState = () => {
  return {
    selectedModel: getDefaultOllamaModel(),
    isThinkingModel: getDefaultOllamaIsThinkingModel(),
    warning: null
  };
};

export async function GET(request: NextRequest) {
  const logger = createRouteLogger("api.user.ollama-model.get", request);
  const startedAt = Date.now();
  try {
    const user = await resolveAuthorizedUser(request);
    if (!user) {
      logger.info("request.unauthorized", { status: 401, durationMs: elapsedMs(startedAt) });
      return NextResponse.json({ error: "Unauthorized" }, { status: 401 });
    }

    const modelState = buildModelState();
    logger.info("request.success", {
      status: 200,
      durationMs: elapsedMs(startedAt),
      userId: user.id,
      selectedModel: modelState.selectedModel,
      isThinkingModel: modelState.isThinkingModel
    });
    return NextResponse.json(modelState, { status: 200 });
  } catch (error) {
    logger.error("request.failed", { status: 500, durationMs: elapsedMs(startedAt), ...toErrorMeta(error) });
    return NextResponse.json({ error: "Failed to load Ollama model settings." }, { status: 500 });
  }
}
