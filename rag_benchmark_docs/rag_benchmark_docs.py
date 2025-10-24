# Copyright (c) KAITO authors.
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.
#!/usr/bin/env python3
"""
RAG vs LLM Benchmark Test

Tests RAG performance improvement over pure LLM using pre-indexed documents

Workflow:
1. Query existing RAG index (user must index documents beforehand)
2. Retrieve document nodes from RAG index
3. Generate test questions (10 closed + 10 open by default)
4. Test both RAG and pure LLM on same questions
5. Judge answers and generate report

Question Types:
- Closed: Factual questions with definitive answers (scored 0/5/10)
- Open: Complex questions requiring comprehensive answers (scored 0-10)

Output:
- questions_{timestamp}.json - Generated test questions
- results_{timestamp}.json - Detailed test results
- report_{timestamp}.json - Performance metrics (JSON)
- report_{timestamp}.txt - Human-readable report

Example:
    python rag_benchmark_docs.py \\
        --index-name my_docs_index \\
        --rag-url http://localhost:5000 \\
        --llm-url http://localhost:8081 \\
        --judge-url http://localhost:8082 \\
        --closed-questions 10 \\
        --open-questions 10 \\
        --output-dir benchmark_results
"""

import argparse
import json
import logging
import os
import random
import time
from dataclasses import asdict, dataclass
from pathlib import Path

import requests
from tqdm import tqdm

# Configure logging
logging.basicConfig(
    level=logging.INFO, format="%(asctime)s - %(levelname)s - %(message)s"
)
logger = logging.getLogger(__name__)


@dataclass
class TestQuestion:
    """Test question with ground truth answer"""

    id: int
    question: str
    answer: str
    question_type: str  # "closed" or "open"


@dataclass
class TestResult:
    """Result for a single test question"""

    question_id: int
    rag_answer: str
    llm_answer: str
    rag_score: float
    llm_score: float
    rag_tokens: int
    llm_tokens: int


@dataclass
class BenchmarkReport:
    """Final benchmark report"""

    total_questions: int
    rag_avg_score: float
    llm_avg_score: float
    rag_closed_avg: float
    llm_closed_avg: float
    rag_open_avg: float
    llm_open_avg: float
    performance_improvement: float
    rag_total_tokens: int
    llm_total_tokens: int
    token_efficiency: float


class RAGBenchmark:
    """Main benchmark testing class"""

    def __init__(
        self,
        rag_url: str,
        llm_url: str,
        judge_llm_url: str,
        llm_model: str = "gpt-4",
        judge_model: str = "gpt-4",
        llm_api_key: str | None = None,
        judge_api_key: str | None = None,
    ):
        """
        Initialize benchmark with service URLs

        Args:
            rag_url: RAGEngine service URL (e.g., http://localhost:8080)
            llm_url: Pure LLM service URL (e.g., http://localhost:8081)
            judge_llm_url: LLM Judge service URL for scoring
            llm_model: Model name for LLM service (default: gpt-4)
            judge_model: Model name for Judge LLM (default: gpt-4)
            llm_api_key: API key for LLM service
            judge_api_key: API key for Judge LLM service
        """
        self.rag_url = rag_url.rstrip("/")
        self.llm_url = llm_url.rstrip("/")
        self.judge_llm_url = judge_llm_url.rstrip("/")
        self.llm_model = llm_model
        self.judge_model = judge_model
        self.llm_api_key = llm_api_key or os.getenv("LLM_API_KEY")
        self.judge_api_key = judge_api_key or os.getenv("JUDGE_API_KEY")
        self.index_name = None
        self.test_questions: list[TestQuestion] = []
        self.test_results: list[TestResult] = []

        # Prepare headers for authenticated requests
        self.llm_headers = self._prepare_headers(self.llm_api_key)
        self.judge_headers = self._prepare_headers(self.judge_api_key)

    def _prepare_headers(self, api_key: str | None) -> dict[str, str]:
        """Prepare headers with API key if provided"""
        headers = {"Content-Type": "application/json"}
        if api_key:
            headers["Authorization"] = f"Bearer {api_key}"
        return headers

    def is_rich_node(self, text: str) -> bool:
        """
        Check if node has rich content based on quality criteria

        Args:
            text: Node text content

        Returns:
            True if node meets quality standards
        """
        if not text or not text.strip():
            return False

        text = text.strip()
        char_count = len(text)
        words = text.split()
        word_count = len(words)

        # Apply quality criteria
        if char_count < 200:
            return False
        if word_count < 30:
            return False
        return not (word_count > 0 and (char_count / word_count) >= 15)

    def generate_test_questions(
        self, num_closed: int = 10, num_open: int = 10
    ) -> list[TestQuestion]:
        """
        Generate test questions from indexed documents using smart sampling strategy
        Retrieves document nodes from RAG index via API

        Args:
            num_closed: Number of closed-ended questions
            num_open: Number of open-ended questions
        """
        total_questions = num_closed + num_open
        logger.info(
            f"Generating {num_closed} closed and {num_open} open questions from index '{self.index_name}'"
        )

        # Step 1: Retrieve all document nodes from RAG index (with pagination)
        logger.info("Retrieving document nodes from RAG index...")
        all_nodes = []
        offset = 0
        limit = 100  # API max limit

        while True:
            response = requests.get(
                f"{self.rag_url}/indexes/{self.index_name}/documents",
                params={"limit": limit, "offset": offset},
            )

            if response.status_code != 200:
                raise Exception(
                    f"Failed to retrieve documents from index: {response.text}"
                )

            batch = response.json().get("documents", [])
            if not batch:
                break  # No more documents

            all_nodes.extend(batch)
            offset += len(batch)

            if len(batch) < limit:
                break  # Last page

        logger.info(f"Retrieved {len(all_nodes)} nodes from index")

        if len(all_nodes) == 0:
            raise Exception(f"No documents found in index '{self.index_name}'")

        # Step 2: Filter content-rich nodes
        rich_nodes = [n for n in all_nodes if self.is_rich_node(n.get("text", ""))]
        logger.info(
            f"Found {len(rich_nodes)} content-rich nodes out of {len(all_nodes)} total nodes"
        )

        if len(rich_nodes) == 0:
            raise Exception("No content-rich nodes found in index")

        # Step 3: Determine node-question assignments
        node_assignments = []  # List of (node, num_questions)

        if len(rich_nodes) <= total_questions:
            # Case A: N ≤ 20 - distribute questions across all rich nodes
            base_questions_per_node = total_questions // len(rich_nodes)
            remainder = total_questions % len(rich_nodes)

            # Assign base questions to all nodes
            for node in rich_nodes:
                node_assignments.append((node, base_questions_per_node))

            # Randomly distribute remainder
            if remainder > 0:
                random_indices = random.sample(range(len(node_assignments)), remainder)
                for idx in random_indices:
                    node, num_q = node_assignments[idx]
                    node_assignments[idx] = (node, num_q + 1)

            logger.info(
                f"Case A: {len(rich_nodes)} nodes ≤ {total_questions} questions"
            )
            logger.info(
                f"Base: {base_questions_per_node} q/node, Remainder: {remainder} extra questions"
            )
        else:
            # Case B: N > 20 - randomly select 20 nodes
            selected_nodes = random.sample(rich_nodes, total_questions)
            node_assignments = [(node, 1) for node in selected_nodes]
            logger.info(
                f"Case B: Randomly selected {total_questions} nodes from {len(rich_nodes)} rich nodes"
            )

        # Step 4: Randomly assign question types
        # Create list of question types [0=closed, 1=open]
        question_types = [0] * num_closed + [1] * num_open
        random.shuffle(question_types)

        # Flatten node assignments to match with question types
        flattened_assignments = []
        for node, num_q in node_assignments:
            for _ in range(num_q):
                flattened_assignments.append(node)

        # Pair each node assignment with question type
        node_type_pairs = list(zip(flattened_assignments, question_types))
        random.shuffle(node_type_pairs)  # Additional shuffle for randomness

        logger.info(f"Created {len(node_type_pairs)} node-question-type assignments")

        # Step 5: Generate questions for each node
        question_id = 0
        for node, q_type in tqdm(node_type_pairs, desc="Generating questions"):
            question_type_name = "closed" if q_type == 0 else "open"
            node_text = node.get("text", "")

            # Generate single question for this node
            prompt = f"""Based on this short text excerpt, generate exactly 1 {"closed-ended (factual, specific answer)" if q_type == 0 else "open-ended (comprehension, analysis)"} question with its answer.

IMPORTANT: This is a short excerpt from a larger document. Ask about specific facts/concepts mentioned in THIS excerpt only.

Text excerpt:
{node_text[:2000]}

Format your response as JSON:
{{
    "question": "...",
    "answer": "..."
}}
"""

            try:
                response = requests.post(
                    f"{self.judge_llm_url}/v1/chat/completions",
                    headers=self.judge_headers,
                    json={
                        "model": self.judge_model,
                        "messages": [{"role": "user", "content": prompt}],
                        "temperature": 0,
                        "max_tokens": 10000,
                    },
                    timeout=60,
                )

                if response.status_code != 200:
                    logger.error(f"Failed to generate question: {response.text}")
                    continue

                result = response.json()
                message = result["choices"][0]["message"]
                content = message.get("content", "") or message.get(
                    "reasoning_content", ""
                )

                if not content:
                    logger.error("No content in response")
                    continue

                # Parse JSON response
                import re

                try:
                    question_data = json.loads(content)
                except json.JSONDecodeError:
                    # Fallback: try to extract JSON
                    json_match = re.search(r"\{.*\}", content, re.DOTALL)
                    if json_match:
                        try:
                            question_data = json.loads(json_match.group())
                        except json.JSONDecodeError:
                            logger.error("Failed to parse JSON")
                            continue
                    else:
                        logger.error("No JSON found")
                        continue

                # Add question
                self.test_questions.append(
                    TestQuestion(
                        id=question_id,
                        question=question_data.get("question", ""),
                        answer=question_data.get("answer", ""),
                        question_type=question_type_name,
                    )
                )
                question_id += 1

                # Small delay to avoid rate limiting
                time.sleep(0.3)

            except Exception as e:
                logger.error(f"Error generating question: {e}")
                continue

        logger.info(f"Successfully generated {len(self.test_questions)} test questions")

        # Verify we have the right distribution
        closed_count = sum(
            1 for q in self.test_questions if q.question_type == "closed"
        )
        open_count = sum(1 for q in self.test_questions if q.question_type == "open")
        logger.info(
            f"Final distribution: {closed_count} closed, {open_count} open questions"
        )

        return self.test_questions

    def query_rag(self, question: str) -> tuple[str, int]:
        """
        Query RAG system with a question

        Returns:
            (answer, token_count)
        """
        # Query RAG system - modeled after rag_solution.py success
        response = requests.post(
            f"{self.rag_url}/v1/chat/completions",
            headers={"Content-Type": "application/json"},
            json={
                "model": self.llm_model,
                "messages": [
                    {
                        "role": "system",
                        "content": "You are a helpful assistant. Answer based on the provided document.",
                    },
                    {"role": "user", "content": question},
                ],
                "index_name": self.index_name,
                "context_token_ratio": 0.7,
                "temperature": 0,
                "max_tokens": 10000,
            },
            timeout=60,
        )

        if response.status_code != 200:
            raise Exception(f"RAG query failed: {response.text}")

        result = response.json()
        message = result["choices"][0]["message"]
        answer = message.get("content", "") or message.get("reasoning_content", "")

        if not answer:
            raise Exception(f"No content in RAG response: {message}")

        # Extract real token usage from RAG API response
        usage = result.get("usage", {})
        tokens = usage.get("total_tokens", 0)

        if tokens == 0:
            # Fallback to estimation if API doesn't return usage
            logger.warning("No token usage in RAG response, using estimation")
            tokens = int(len(answer.split()) * 1.3 + len(question.split()) * 1.3)

        return answer, tokens

    def query_llm(self, question: str) -> tuple[str, int]:
        """
        Query pure LLM with a question

        Returns:
            (answer, token_count)
        """
        response = requests.post(
            f"{self.llm_url}/v1/chat/completions",
            headers=self.llm_headers,
            json={
                "model": self.llm_model,
                "messages": [{"role": "user", "content": question}],
                "temperature": 0,
                "max_tokens": 10000,
            },
            timeout=3000,  # Very long timeout for LLM with reasoning
        )

        if response.status_code != 200:
            raise Exception(f"LLM query failed: {response.text}")

        result = response.json()
        message = result["choices"][0]["message"]
        answer = message.get("content", "") or message.get("reasoning_content", "")

        if not answer:
            raise Exception(f"No content in LLM response: {message}")

        # Extract real token usage from LLM API response
        usage = result.get("usage", {})
        tokens = usage.get("total_tokens", 0)

        if tokens == 0:
            # Fallback to estimation if API doesn't return usage
            logger.warning("No token usage in LLM response, using estimation")
            tokens = int(len(answer.split()) * 1.3 + len(question.split()) * 1.3)

        return answer, tokens

    def score_answer(
        self, question: str, ground_truth: str, answer: str, question_type: str
    ) -> float:
        """
        Use LLM Judge to score an answer against ground truth

        Args:
            question: The test question
            ground_truth: Expected answer
            answer: Provided answer
            question_type: "closed" or "open"

        Returns:
            Score from 0-10
        """
        if question_type == "closed":
            # Strict binary-like scoring for closed-ended questions
            prompt = f"""Score this factual answer using STRICT criteria:
            - Give 10 points if the answer is COMPLETELY CORRECT with all key facts matching
            - Give 5 points if the answer is PARTIALLY CORRECT (has some correct facts but missing important details)
            - Give 0 points if the answer is WRONG or completely misses the point
            
            Question: {question}
            Ground Truth Answer: {ground_truth}
            Provided Answer: {answer}
            
            This is a CLOSED-ENDED factual question. Be strict - if the core fact is wrong, give 0.
            Respond with only a number: 0, 5, or 10."""
        else:
            # Gradient scoring for open-ended questions
            prompt = f"""Score this analytical answer on a scale of 0-10 considering:
            - Accuracy (3 points): Are the facts and concepts correct?
            - Completeness (3 points): Does it cover the main points from the ground truth?
            - Understanding (2 points): Does it show comprehension of the topic?
            - Relevance (2 points): Does it directly address the question?
            
            Question: {question}
            Ground Truth Answer: {ground_truth}
            Provided Answer: {answer}
            
            This is an OPEN-ENDED analytical question. Provide a nuanced score from 0-10.
            Respond with only a number from 0 to 10."""

        response = requests.post(
            f"{self.judge_llm_url}/v1/chat/completions",
            headers=self.judge_headers,
            json={
                "model": self.judge_model,
                "messages": [{"role": "user", "content": prompt}],
                "temperature": 0,
                "max_tokens": 10000,  # Allow full reasoning output
            },
            timeout=180,
        )

        if response.status_code != 200:
            logger.error(f"Scoring failed: {response.text}")
            return 0.0

        result = response.json()
        message = result["choices"][0]["message"]
        score_text = (
            message.get("content", "") or message.get("reasoning_content", "")
        ).strip()

        if not score_text:
            logger.error(f"No content in Judge LLM scoring response: {message}")
            return 0.0

        # Extract numeric score from response (handle reasoning models that output verbose text)
        import re

        # Strategy 1: Look for explicit score patterns like "Score: 8" or "Final score: 10"
        score_patterns = [
            r"(?:final\s*)?score\s*[:=]\s*(\d+(?:\.\d+)?)",  # "Score: 8" or "Final score: 10"
            r"(\d+(?:\.\d+)?)\s*(?:out of|/)\s*10",  # "8 out of 10" or "8/10"
            r"(?:rating|score).*?(\d+(?:\.\d+)?)",  # "rating is 8" or "score is 8"
        ]

        for pattern in score_patterns:
            matches = re.findall(pattern, score_text, re.IGNORECASE)
            if matches:
                try:
                    score = float(matches[-1])  # Use the last match
                    logger.info(f"Extracted score {score} from pattern: {pattern}")
                    # For closed questions, enforce 0/5/10 scoring
                    if question_type == "closed":
                        if score >= 8:
                            return 10.0
                        elif score >= 3:
                            return 5.0
                        else:
                            return 0.0
                    # For open questions, clamp to 0-10 range
                    return min(max(score, 0.0), 10.0)
                except (ValueError, IndexError):
                    continue  # Try next pattern

        # Strategy 2: Fallback - extract all numbers and use the last one in valid range
        all_numbers = re.findall(r"\b(\d+(?:\.\d+)?)\b", score_text)
        for num in reversed(all_numbers):  # Check from end to start
            try:
                score = float(num)
                if 0 <= score <= 10:  # Must be valid score range
                    logger.info(
                        f"Extracted score {score} from fallback (last valid number)"
                    )
                    if question_type == "closed":
                        if score >= 8:
                            return 10.0
                        elif score >= 3:
                            return 5.0
                        else:
                            return 0.0
                    return min(max(score, 0.0), 10.0)
            except ValueError:
                continue

        logger.error(f"Could not extract valid score from: {score_text[:300]}")
        return 0.0

    def run_benchmark(self) -> BenchmarkReport:
        """Run the full benchmark test"""
        logger.info("Starting benchmark tests")

        for question in tqdm(self.test_questions, desc="Testing questions"):
            # Query both systems
            rag_answer, rag_tokens = self.query_rag(question.question)
            llm_answer, llm_tokens = self.query_llm(question.question)

            # Score both answers with question type awareness
            rag_score = self.score_answer(
                question.question, question.answer, rag_answer, question.question_type
            )
            llm_score = self.score_answer(
                question.question, question.answer, llm_answer, question.question_type
            )

            # Store results
            result = TestResult(
                question_id=question.id,
                rag_answer=rag_answer,
                llm_answer=llm_answer,
                rag_score=rag_score,
                llm_score=llm_score,
                rag_tokens=rag_tokens,
                llm_tokens=llm_tokens,
            )
            self.test_results.append(result)

            # Small delay to avoid rate limiting
            time.sleep(0.5)

        return self.generate_report()

    def generate_report(self) -> BenchmarkReport:
        """Generate final benchmark report"""
        # Separate closed and open questions
        closed_results = [
            r
            for r in self.test_results
            if self.test_questions[r.question_id].question_type == "closed"
        ]
        open_results = [
            r
            for r in self.test_results
            if self.test_questions[r.question_id].question_type == "open"
        ]

        # Calculate averages
        rag_avg = sum(r.rag_score for r in self.test_results) / len(self.test_results)
        llm_avg = sum(r.llm_score for r in self.test_results) / len(self.test_results)

        rag_closed_avg = (
            sum(r.rag_score for r in closed_results) / len(closed_results)
            if closed_results
            else 0
        )
        llm_closed_avg = (
            sum(r.llm_score for r in closed_results) / len(closed_results)
            if closed_results
            else 0
        )

        rag_open_avg = (
            sum(r.rag_score for r in open_results) / len(open_results)
            if open_results
            else 0
        )
        llm_open_avg = (
            sum(r.llm_score for r in open_results) / len(open_results)
            if open_results
            else 0
        )

        # Token usage
        rag_tokens = sum(r.rag_tokens for r in self.test_results)
        llm_tokens = sum(r.llm_tokens for r in self.test_results)

        # Performance improvement
        improvement = ((rag_avg - llm_avg) / llm_avg * 100) if llm_avg > 0 else 0
        token_efficiency = (
            ((llm_tokens - rag_tokens) / llm_tokens * 100) if llm_tokens > 0 else 0
        )

        report = BenchmarkReport(
            total_questions=len(self.test_questions),
            rag_avg_score=rag_avg,
            llm_avg_score=llm_avg,
            rag_closed_avg=rag_closed_avg,
            llm_closed_avg=llm_closed_avg,
            rag_open_avg=rag_open_avg,
            llm_open_avg=llm_open_avg,
            performance_improvement=improvement,
            rag_total_tokens=rag_tokens,
            llm_total_tokens=llm_tokens,
            token_efficiency=token_efficiency,
        )

        return report

    def save_results(self, output_dir: str = "benchmark_results"):
        """Save all results to files"""
        Path(output_dir).mkdir(exist_ok=True)
        timestamp = time.strftime("%Y%m%d_%H%M%S")

        # Save test questions
        with open(f"{output_dir}/questions_{timestamp}.json", "w") as f:
            json.dump([asdict(q) for q in self.test_questions], f, indent=2)

        # Save test results
        with open(f"{output_dir}/results_{timestamp}.json", "w") as f:
            json.dump([asdict(r) for r in self.test_results], f, indent=2)

        # Save report
        report = self.generate_report()
        with open(f"{output_dir}/report_{timestamp}.json", "w") as f:
            json.dump(asdict(report), f, indent=2)

        # Save human-readable report
        with open(f"{output_dir}/report_{timestamp}.txt", "w") as f:
            f.write("RAG vs LLM Benchmark Report\n")
            f.write("=" * 50 + "\n\n")
            f.write(f"Total Questions: {report.total_questions}\n\n")
            f.write("Average Scores (0-10):\n")
            f.write(f"  RAG Overall: {report.rag_avg_score:.2f}\n")
            f.write(f"  LLM Overall: {report.llm_avg_score:.2f}\n")
            f.write(
                f"  Performance Improvement: {report.performance_improvement:.1f}%\n\n"
            )
            f.write("Closed Questions (0/5/10 scoring - factual accuracy):\n")
            f.write(f"  RAG: {report.rag_closed_avg:.2f}\n")
            f.write(f"  LLM: {report.llm_closed_avg:.2f}\n\n")
            f.write("Open Questions (0-10 scoring - comprehensive evaluation):\n")
            f.write(f"  RAG: {report.rag_open_avg:.2f}\n")
            f.write(f"  LLM: {report.llm_open_avg:.2f}\n\n")
            f.write("Token Usage:\n")
            f.write(f"  RAG: {report.rag_total_tokens:,} tokens\n")
            f.write(f"  LLM: {report.llm_total_tokens:,} tokens\n")
            f.write(
                f"  Efficiency: {report.token_efficiency:.1f}% {'fewer' if report.token_efficiency > 0 else 'more'} tokens with RAG\n"
            )

        logger.info(f"Results saved to {output_dir}")


def main():
    """Main execution function"""
    parser = argparse.ArgumentParser(description="RAG vs LLM Benchmark Testing")
    parser.add_argument(
        "--index-name", required=True, help="Existing RAG index name (required)"
    )
    parser.add_argument(
        "--rag-url", default="http://localhost:5000", help="RAG service URL"
    )
    parser.add_argument(
        "--llm-url", default="http://localhost:8081", help="LLM service URL"
    )
    parser.add_argument(
        "--judge-url", default="http://localhost:8082", help="Judge LLM service URL"
    )
    parser.add_argument(
        "--llm-model",
        default="gpt-4",
        help="Model name for LLM/RAG service (default: gpt-4)",
    )
    parser.add_argument(
        "--judge-model",
        default="gpt-4",
        help="Model name for Judge LLM (default: gpt-4)",
    )
    parser.add_argument(
        "--llm-api-key", help="API key for LLM service (or set LLM_API_KEY env var)"
    )
    parser.add_argument(
        "--judge-api-key", help="API key for Judge LLM (or set JUDGE_API_KEY env var)"
    )
    parser.add_argument(
        "--closed-questions", type=int, default=10, help="Number of closed questions"
    )
    parser.add_argument(
        "--open-questions", type=int, default=10, help="Number of open questions"
    )
    parser.add_argument(
        "--output-dir", default="benchmark_results", help="Output directory"
    )

    args = parser.parse_args()

    # Initialize benchmark with API keys and models
    benchmark = RAGBenchmark(
        args.rag_url,
        args.llm_url,
        args.judge_url,
        args.llm_model,
        args.judge_model,
        args.llm_api_key,
        args.judge_api_key,
    )

    try:
        # Phase 1: Use existing index
        logger.info(f"Using existing index: {args.index_name}")
        benchmark.index_name = args.index_name

        # Phase 2: Generate test questions (fetch nodes from RAG index via API)
        benchmark.generate_test_questions(args.closed_questions, args.open_questions)

        # Phase 3: Run benchmark
        report = benchmark.run_benchmark()

        # Phase 4: Save results
        benchmark.save_results(args.output_dir)

        # Print summary
        print("\n" + "=" * 50)
        print("BENCHMARK COMPLETE")
        print("=" * 50)
        print(f"RAG Average Score: {report.rag_avg_score:.2f}/10")
        print(f"LLM Average Score: {report.llm_avg_score:.2f}/10")
        print(f"Performance Improvement: {report.performance_improvement:.1f}%")
        print(f"Results saved to: {args.output_dir}")

        return 0

    except Exception as e:
        logger.error(f"Benchmark failed: {e}")
        return 1


if __name__ == "__main__":
    exit(main())
