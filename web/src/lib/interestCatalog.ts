export type InterestOption = {
  id: string;
  emoji: string;
  label: string;
};

export const MAX_SELECTED_INTERESTS = 10;

// Keep stable IDs for saved profiles. Labels describe a subject people can talk about.
export const INTEREST_OPTIONS: InterestOption[] = [
  { id: "travel", emoji: "✈️", label: "Travel and places" },
  { id: "technology", emoji: "💻", label: "Technology and inventions" },
  { id: "aviation", emoji: "🛫", label: "Aircraft and flying" },
  { id: "fitness", emoji: "🏃", label: "Exercise and fitness" },
  { id: "music", emoji: "🎵", label: "Music and concerts" },
  { id: "books", emoji: "📚", label: "Books and reading" },
  { id: "movies", emoji: "🎬", label: "Movies and TV" },
  { id: "food", emoji: "🍜", label: "Food, drinks and desserts" },
  { id: "sport", emoji: "⚽", label: "Sports and teams" },
  { id: "career", emoji: "🚀", label: "Jobs and careers" },
  { id: "languages", emoji: "🗣️", label: "Learning languages" },
  { id: "gaming", emoji: "🎮", label: "Video games" },
  { id: "photography", emoji: "📷", label: "Taking photos" },
  { id: "design", emoji: "🎨", label: "Design and creative projects" },
  { id: "cooking", emoji: "👨‍🍳", label: "Cooking and baking" },
  { id: "psychology", emoji: "🧠", label: "People and behavior" },
  { id: "productivity", emoji: "⏱️", label: "Habits and routines" },
  { id: "minimalism", emoji: "🧱", label: "Simple living and minimalism" },
  { id: "ai", emoji: "🤖", label: "AI in daily life" },
  { id: "science", emoji: "🔬", label: "Science and discoveries" },
  { id: "mathematics", emoji: "➗", label: "Mathematics and puzzles" },
  { id: "physics", emoji: "⚛️", label: "Physics in everyday life" },
  { id: "chemistry", emoji: "🧪", label: "Chemistry in everyday life" },
  { id: "biology", emoji: "🧬", label: "Biology and living things" },
  { id: "history", emoji: "🏛️", label: "History and historic places" },
  { id: "nature", emoji: "🌿", label: "Nature and wildlife" },
  { id: "hiking", emoji: "🥾", label: "Hiking and trails" },
  { id: "fashion", emoji: "👗", label: "Fashion and personal style" },
  { id: "art", emoji: "🖼️", label: "Art and cultural events" },
  { id: "finance", emoji: "💰", label: "Money and investing" },
  { id: "economics", emoji: "🏦", label: "How economies work" },
  { id: "real-estate", emoji: "🏘️", label: "Homes and property" },
  { id: "pets", emoji: "🐶", label: "Pets and animals" },
  { id: "parenting", emoji: "👨‍👩‍👧", label: "Parenting and family life" },
  { id: "education", emoji: "🎓", label: "School and learning" },
  { id: "volunteering", emoji: "🤝", label: "Helping your community" },
  { id: "business", emoji: "📈", label: "Starting and running a business" },
  { id: "marketing", emoji: "📣", label: "Marketing and advertising" },
  { id: "public-speaking", emoji: "🎤", label: "Speaking in public" },
  { id: "remote-work", emoji: "🏠", label: "Working from home" },
  { id: "health", emoji: "❤️", label: "Healthy living" },
  { id: "medicine", emoji: "🩺", label: "Medicine and healthcare" },
  { id: "mental-health", emoji: "💚", label: "Mental well-being" },
  { id: "gardening", emoji: "🌻", label: "Gardening and plants" },
  { id: "diy", emoji: "🛠️", label: "DIY and home projects" },
  { id: "cars", emoji: "🚗", label: "Cars and motorcycles" },
  { id: "board-games", emoji: "🎲", label: "Chess, board and card games" },
  { id: "dance", emoji: "💃", label: "Dancing" },
  { id: "theater", emoji: "🎭", label: "Theater and live comedy" },
  { id: "writing", emoji: "✍️", label: "Writing stories and poems" },
  { id: "journaling", emoji: "📝", label: "Personal journaling" },
  { id: "climate", emoji: "🌍", label: "Climate and the environment" },
  { id: "space", emoji: "🛰️", label: "Space and astronomy" },
  { id: "programming", emoji: "⌨️", label: "Writing software" },
  { id: "data-science", emoji: "📉", label: "Working with data" },
  { id: "cybersecurity", emoji: "🛡️", label: "Online privacy and safety" },
  { id: "content-creation", emoji: "🎥", label: "Online content and podcasts" },
  { id: "audio-production", emoji: "🎛️", label: "Making and editing audio" },
  { id: "nutrition", emoji: "🥗", label: "Food choices and nutrition" },
  { id: "architecture", emoji: "🏗️", label: "Buildings and interiors" },
  { id: "philosophy", emoji: "📖", label: "Big questions and philosophy" },
  { id: "law", emoji: "⚖️", label: "Law and justice in society" },
  { id: "news", emoji: "📰", label: "Following and understanding news" },
  { id: "geopolitics", emoji: "🗺️", label: "Countries and global affairs" },
  { id: "sailing", emoji: "⛵", label: "Sailing and life on the water" }
];

// Every removed ID from the original catalog maps to a closely related visible
// theme, so older profile selections remain usable after the catalog changes.
export const LEGACY_INTEREST_ALIASES: Record<string, string> = {
  startups: "business",
  cycling: "fitness",
  swimming: "fitness",
  yoga: "fitness",
  investing: "finance",
  crypto: "finance",
  "self-development": "productivity",
  culture: "art",
  entrepreneurship: "business",
  mindfulness: "mental-health",
  podcasts: "content-creation",
  sustainability: "climate",
  astronomy: "space",
  robotics: "technology",
  "web-development": "programming",
  "mobile-development": "programming",
  "machine-learning": "ai",
  "home-decor": "architecture",
  woodworking: "diy",
  motorcycles: "cars",
  chess: "board-games",
  "card-games": "board-games",
  comedy: "theater",
  poetry: "writing",
  "language-teaching": "languages",
  backpacking: "travel",
  "luxury-travel": "travel",
  coffee: "food",
  tea: "food",
  baking: "cooking",
  desserts: "food",
  "street-food": "food",
  "vegan-living": "nutrition",
  "interior-design": "architecture",
  "social-media": "content-creation"
};

const interestLookup = new Map(INTEREST_OPTIONS.map((option) => [option.id, option]));

export const getInterestOption = (id: string): InterestOption | undefined => interestLookup.get(id);

export const normalizeInterestIds = (value: unknown): string[] => {
  if (!Array.isArray(value)) {
    return [];
  }

  const seen = new Set<string>();
  const normalized: string[] = [];

  for (const item of value) {
    if (typeof item !== "string") {
      continue;
    }

    const legacyId = item.trim().toLowerCase();
    const id = LEGACY_INTEREST_ALIASES[legacyId] ?? legacyId;
    if (!interestLookup.has(id) || seen.has(id)) {
      continue;
    }

    seen.add(id);
    normalized.push(id);

    if (normalized.length >= MAX_SELECTED_INTERESTS) {
      break;
    }
  }

  return normalized;
};

export const resolveInterestLabels = (interestIds: string[]): string[] =>
  normalizeInterestIds(interestIds).flatMap((id) => {
    const option = interestLookup.get(id);
    return option ? [option.label] : [];
  });
