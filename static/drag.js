const socket = new WebSocket('ws://localhost:18080/ws');
socket.onopen = () => {
    console.log("webSocket connection established")
}
document.addEventListener("mousedown", function (e) {
    const target = e.target;
    if (!target.classList.contains("unit")) return;
  
    let offsetX = e.clientX - target.offsetLeft;
    let offsetY = e.clientY - target.offsetTop;
  
    function move(e) {
      target.style.left = `${e.clientX - offsetX}px`;
      target.style.top = `${e.clientY - offsetY}px`;
      updateUnitPositionOnServer();
    }
  
    function up() {
      document.removeEventListener("mousemove", move);
      document.removeEventListener("mouseup", up);
      target.style.cursor = "grab";
      console.log("up: updating position")
      updateUnitPositionOnServer();
    }

    function updateUnitPositionOnServer(){
        if (socket.readyState === WebSocket.OPEN) {
            let data = {
                Left: parseInt(target.style.left, 10),
                Top:  parseInt(target.style.top, 10),
                ID:    target.id
            };
            socket.send(JSON.stringify(data));
            console.log(data);
        }
        else{
            console.log("can't connect to server via websocket")
        }
    }
  
    document.addEventListener("mousemove", move);
    document.addEventListener("mouseup", up);
    target.style.cursor = "grabbing";
  });