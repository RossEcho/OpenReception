(function () {
  function initializeOnboarding() {
    var slides = Array.prototype.slice.call(document.querySelectorAll(".onboarding-slide"));
    if (!slides.length) return;
    var dots = Array.prototype.slice.call(document.querySelectorAll(".onboarding-dots i"));
    var previous = document.getElementById("onboarding-previous");
    var next = document.getElementById("onboarding-next");
    var form = document.getElementById("onboarding-complete");
    var destination = document.getElementById("onboarding-destination");
    var current = 0;

    function render() {
      slides.forEach(function (slide, index) { slide.classList.toggle("active", index === current); });
      dots.forEach(function (dot, index) { dot.classList.toggle("active", index === current); });
      previous.disabled = current === 0;
      next.hidden = current === slides.length - 1;
    }

    previous.addEventListener("click", function () {
      if (current > 0) current -= 1;
      render();
    });
    next.addEventListener("click", function () {
      if (current < slides.length - 1) current += 1;
      render();
    });
    document.querySelectorAll("[data-finish]").forEach(function (button) {
      button.addEventListener("click", function () {
        destination.value = button.dataset.finish;
        form.submit();
      });
    });
    render();
  }

  if (document.readyState === "loading") document.addEventListener("DOMContentLoaded", initializeOnboarding);
  else initializeOnboarding();
})();
