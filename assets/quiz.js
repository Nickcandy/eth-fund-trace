document.querySelectorAll("[data-quiz]").forEach((quiz) => {
  const feedback = quiz.querySelector(".feedback");
  quiz.querySelectorAll("button[data-answer]").forEach((button) => {
    button.addEventListener("click", () => {
      const correct = button.dataset.answer === quiz.dataset.correct;
      feedback.textContent = correct ? quiz.dataset.yes : quiz.dataset.no;
      feedback.className = `feedback ${correct ? "correct" : "incorrect"}`;
    });
  });
});
