import { NextResponse, type NextRequest } from "next/server";
import { SESSION_COOKIE_NAME, getUserBySessionToken } from "../../../../src/server/auth";
import {
  DEFAULT_OLLAMA_BASE_URL,
  fetchLocalOllamaModels,
  getDefaultOllamaModel,
  getUserPreferredOllamaModel,
  mergeModelList,
  saveUserPreferredOllamaModel
} from "../../../../src/server/ollama";
import { createRouteLogger, elapsedMs, toErrorMeta } from "../../../../src/server/logger";

export const runtime = "nodejs";
export const dynamic = "force-dynamic";

type UpdateModelPayload = {
  model?: unknown;
};

const resolveAuthorizedUser = async (request: NextRequest) => {
  const token = request.cookies.get(SESSION_COOKIE_NAME)?.value;
  return getUserBySessionToken(token);
};

const buildModelState = async (
  userId: string,
  logger?: ReturnType<typeof createRouteLogger>
) => {
  const baseUrl = process.env.OLLAMA_BASE_URL ?? DEFAULT_OLLAMA_BASE_URL;
  const defaultModel = getDefaultOllamaModel();
  const selectedModel = (await getUserPreferredOllamaModel(userId)) ?? defaultModel;

  try {
    const availableModels = await fetchLocalOllamaModels(baseUrl);
    logger?.debug("models.refreshed", {
      baseUrl,
      selectedModel,
      availableCount: availableModels.length
    });
    return {
      selectedModel,
      availableModels: mergeModelList([selectedModel], availableModels, [defaultModel]),
      warning: null
    };
  } catch (error) {
    logger?.warn("models.refresh_failed", {
      baseUrl,
      selectedModel,
      ...toErrorMeta(error)
    });
    return {
      selectedModel,
      availableModels: mergeModelList([selectedModel], [defaultModel]),
      warning: "Could not refresh models from local Ollama. Showing cached/default list."
    };
  }
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

    const modelState = await buildModelState(user.id, logger);
    logger.info("request.success", {
      status: 200,
      durationMs: elapsedMs(startedAt),
      userId: user.id,
      selectedModel: modelState.selectedModel,
      availableCount: modelState.availableModels.length
    });
    return NextResponse.json(modelState, { status: 200 });
  } catch (error) {
    logger.error("request.failed", { status: 500, durationMs: elapsedMs(startedAt), ...toErrorMeta(error) });
    return NextResponse.json({ error: "Failed to load Ollama model settings." }, { status: 500 });
  }
}

export async function PUT(request: NextRequest) {
  const logger = createRouteLogger("api.user.ollama-model.put", request);
  const startedAt = Date.now();
  try {
    const user = await resolveAuthorizedUser(request);
    if (!user) {
      logger.info("request.unauthorized", { status: 401, durationMs: elapsedMs(startedAt) });
      return NextResponse.json({ error: "Unauthorized" }, { status: 401 });
    }

    const payload = (await request.json().catch(() => null)) as UpdateModelPayload | null;
    const model = typeof payload?.model === "string" ? payload.model : "";
    await saveUserPreferredOllamaModel(user.id, model);

    const modelState = await buildModelState(user.id, logger);
    logger.info("request.success", {
      status: 200,
      durationMs: elapsedMs(startedAt),
      userId: user.id,
      selectedModel: modelState.selectedModel,
      availableCount: modelState.availableModels.length
    });
    return NextResponse.json(modelState, { status: 200 });
  } catch (error) {
    if (error instanceof Error && error.message === "Model name is invalid.") {
      logger.warn("request.rejected", { status: 400, durationMs: elapsedMs(startedAt), reason: error.message });
      return NextResponse.json({ error: "Model name is invalid." }, { status: 400 });
    }

    logger.error("request.failed", { status: 500, durationMs: elapsedMs(startedAt), ...toErrorMeta(error) });
    return NextResponse.json({ error: "Failed to save Ollama model settings." }, { status: 500 });
  }
}
