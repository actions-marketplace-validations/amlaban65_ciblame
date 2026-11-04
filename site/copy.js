// Adds a Copy button to each command block.
// Output samples are marked ```text in the guides and get no button.
document.querySelectorAll("pre > code:not(.language-text)").forEach(function (code) {
  var button = document.createElement("button");
  button.type = "button";
  button.className = "copy";
  button.textContent = "Copy";
  button.addEventListener("click", function () {
    var text = code.innerText.replace(/\n$/, "");
    var done = function () {
      button.textContent = "Copied";
      setTimeout(function () { button.textContent = "Copy"; }, 1500);
    };
    if (navigator.clipboard) {
      navigator.clipboard.writeText(text).then(done, function () { button.textContent = "Select and copy"; });
    }
  });
  code.parentNode.classList.add("has-copy");
  code.parentNode.appendChild(button);
});
