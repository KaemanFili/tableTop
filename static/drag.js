document.addEventListener("mousedown", function (e) {
    const target = e.target;
    if (!target.classList.contains("unit")) return;
  
    let offsetX = e.clientX - target.offsetLeft;
    let offsetY = e.clientY - target.offsetTop;
  
    function move(e) {
      target.style.left = `${e.clientX - offsetX}px`;
      target.style.top = `${e.clientY - offsetY}px`;
    }
  
    function up() {
      document.removeEventListener("mousemove", move);
      document.removeEventListener("mouseup", up);
      target.style.cursor = "grab";
    }
  
    document.addEventListener("mousemove", move);
    document.addEventListener("mouseup", up);
    target.style.cursor = "grabbing";
  });