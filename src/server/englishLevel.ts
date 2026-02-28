import { DEFAULT_ENGLISH_LEVEL, parseEnglishLevel, type EnglishLevel } from "../lib/englishLevel";
import { query } from "./db";

type UserEnglishLevelRow = {
  english_level: string | null;
};

export const getUserEnglishLevel = async (userId: string): Promise<EnglishLevel> => {
  const result = await query<UserEnglishLevelRow>(
    `SELECT english_level
     FROM users
     WHERE id = $1
     LIMIT 1`,
    [userId]
  );

  return parseEnglishLevel(result.rows[0]?.english_level) ?? DEFAULT_ENGLISH_LEVEL;
};

export const saveUserEnglishLevel = async (userId: string, level: string): Promise<EnglishLevel> => {
  const normalized = parseEnglishLevel(level);
  if (!normalized) {
    throw new Error("English level is invalid.");
  }

  await query(
    `UPDATE users
     SET english_level = $2
     WHERE id = $1`,
    [userId, normalized]
  );

  return normalized;
};
