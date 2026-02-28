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
