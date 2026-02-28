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

export const runtime = "nodejs";
export const dynamic = "force-dynamic";

type UpdateModelPayload = {
  model?: unknown;
};

const resolveAuthorizedUser = async (request: NextRequest) => {
  const token = request.cookies.get(SESSION_COOKIE_NAME)?.value;
  return getUserBySessionToken(token);
};

const buildModelState = async (userId: string) => {
  const baseUrl = process.env.OLLAMA_BASE_URL ?? DEFAULT_OLLAMA_BASE_URL;
  const defaultModel = getDefaultOllamaModel();
  const selectedModel = (await getUserPreferredOllamaModel(userId)) ?? defaultModel;

  try {
    const availableModels = await fetchLocalOllamaModels(baseUrl);
    return {
      selectedModel,
      availableModels: mergeModelList([selectedModel], availableModels, [defaultModel]),
      warning: null
    };
  } catch {
    return {
      selectedModel,
      availableModels: mergeModelList([selectedModel], [defaultModel]),
      warning: "Could not refresh models from local Ollama. Showing cached/default list."
    };
  }
};

export async function GET(request: NextRequest) {
  try {
    const user = await resolveAuthorizedUser(request);
    if (!user) {
      return NextResponse.json({ error: "Unauthorized" }, { status: 401 });
    }

    const modelState = await buildModelState(user.id);
    return NextResponse.json(modelState, { status: 200 });
  } catch (error) {
    console.error("User Ollama model route failed", error);
    return NextResponse.json({ error: "Failed to load Ollama model settings." }, { status: 500 });
  }
}

export async function PUT(request: NextRequest) {
  try {
    const user = await resolveAuthorizedUser(request);
    if (!user) {
      return NextResponse.json({ error: "Unauthorized" }, { status: 401 });
    }

    const payload = (await request.json().catch(() => null)) as UpdateModelPayload | null;
    const model = typeof payload?.model === "string" ? payload.model : "";
    await saveUserPreferredOllamaModel(user.id, model);

    const modelState = await buildModelState(user.id);
    return NextResponse.json(modelState, { status: 200 });
  } catch (error) {
    if (error instanceof Error && error.message === "Model name is invalid.") {
      return NextResponse.json({ error: "Model name is invalid." }, { status: 400 });
    }

    console.error("User Ollama model update route failed", error);
    return NextResponse.json({ error: "Failed to save Ollama model settings." }, { status: 500 });
  }
}
