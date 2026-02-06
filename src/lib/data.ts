export type TopicData = {
  questions: string[];
  words: string[];
};

export type Suggestion = {
  wrong: string;
  right: string;
  explanation: string;
};

export type Recording = {
  id: string;
  topic: string;
  duration: number;
  timestamp: string;
  transcript: string;
  suggestions: Suggestion[];
};

const TOPIC_DATABASE: Record<string, TopicData> = {
  "Free talk": {
    questions: [],
    words: []
  },
  "My morning routine": {
    questions: [
      "What time do you usually wake up?",
      "What is the first thing you do?",
      "How do you feel in the morning?",
      "What do you have for breakfast?"
    ],
    words: ["alarm", "routine", "energize", "habit", "refresh", "prepare", "morning"]
  },
  "Favorite hobby": {
    questions: [
      "When did you start this hobby?",
      "Why do you enjoy it?",
      "How often do you do it?",
      "Do you do it alone or with others?"
    ],
    words: ["passion", "hobby", "practice", "skill", "enjoy", "favorite"]
  },
  "Learning English": {
    questions: [
      "How long have you been learning?",
      "What methods work best for you?",
      "What challenges do you face?",
      "What are your goals?"
    ],
    words: ["progress", "challenge", "fluent", "improve", "practice", "language", "goal"]
  },
  "Dream vacation": {
    questions: [
      "Where do you want to go and why?",
      "Who would you travel with?",
      "What would you do there?",
      "How long would you stay?"
    ],
    words: ["destination", "itinerary", "scenery", "adventure", "culture", "explore"]
  },
  "Best friend": {
    questions: [
      "How did you meet your best friend?",
      "What makes this friendship special?",
      "What do you usually do together?",
      "What have you learned from this friend?"
    ],
    words: ["trust", "support", "memories", "loyal", "connection", "respect"]
  },
  "Career goals": {
    questions: [
      "What role do you want in the next 3 years?",
      "Which skills are most important for your goal?",
      "What challenges do you expect?",
      "How are you preparing now?"
    ],
    words: ["promotion", "impact", "leadership", "growth", "strategy", "milestone"]
  }
};

const TRANSCRIPT_SAMPLES: Record<string, string> = {
  "Free talk":
    "Today I want to talk about something I have been thinking about lately. I think it is important to take time every day to reflect on our progress and what we have learned. Speaking freely like this helps us practice naturally without worrying too much about mistakes.",
  "My morning routine":
    "I usually wake up at 6:30 AM. The first thing I do is drink a glass of water to hydrate myself. Then I do some light exercise or stretching for about 15 minutes. After that, I take a shower and have breakfast with tea. This routine helps me feel energized for the day.",
  "Favorite hobby":
    "My favorite hobby is reading. I enjoy reading books in different genres, especially science fiction and mystery novels. I try to read for at least 30 minutes every day before bed. It helps me relax and expand my vocabulary at the same time. I have been reading since I was a child.",
  "Learning English":
    "I have been learning English for about 5 years now. At first, I struggled with speaking, but I found that practicing daily conversations has helped me improve significantly. The biggest challenge is understanding native speakers when they talk fast. My goal is to reach a fluent level where I can express complex ideas effortlessly.",
  "Dream vacation":
    "My dream vacation is a two-week trip to Japan during spring. I would like to visit Tokyo and Kyoto, try local food, and explore traditional neighborhoods. I also want to improve my photography while traveling. It would be a perfect mix of culture, history, and modern life.",
  "Best friend":
    "I met my best friend at university. We were in the same study group and quickly realized we shared similar values. Over time, we supported each other through difficult periods and celebrated many milestones together. This friendship taught me how important trust and honest communication are.",
  "Career goals":
    "My main career goal is to become a product-focused engineer who can lead complex features from idea to launch. To do that, I am improving both technical and communication skills. I practice explaining tradeoffs clearly and I actively seek feedback from my team. I believe consistent growth is the key to long-term success."
};

const DEFAULT_SUGGESTIONS: Suggestion[] = [
  {
    wrong: "I think it is important to taking time",
    right: "I think it is important to take time",
    explanation: "After \"to\" use the base form of the verb, not the gerund."
  },
  {
    wrong: "help us practice naturally without worry about mistakes",
    right: "help us practice naturally without worrying about mistakes",
    explanation: "Use the gerund form after the preposition \"without\"."
  },
  {
    wrong: "I am reading since I was a child",
    right: "I have been reading since I was a child",
    explanation: "Use present perfect continuous for actions that started in the past and continue now."
  },
  {
    wrong: "it helps me relax and expand the vocabulary",
    right: "it helps me relax and expand my vocabulary",
    explanation: "Use the possessive pronoun \"my\" in this context."
  }
];

export const ALL_TOPICS = [
  "My morning routine",
  "Favorite hobby",
  "Learning English",
  "Dream vacation",
  "Best friend",
  "Career goals"
];

export const pickRandomTopics = (count = 3): string[] => {
  return [...ALL_TOPICS]
    .sort(() => Math.random() - 0.5)
    .slice(0, count);
};

export const getTopicData = (topic: string): TopicData => {
  return TOPIC_DATABASE[topic] ?? { questions: [], words: [] };
};

export const generateTranscript = (topic: string): string => {
  return TRANSCRIPT_SAMPLES[topic] ?? "This is a sample transcript of a speaking practice session.";
};

export const generateSuggestions = (): Suggestion[] => {
  return DEFAULT_SUGGESTIONS;
};
