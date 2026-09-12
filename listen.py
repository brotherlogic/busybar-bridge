import asyncio
import websockets
import json

# This imports the compiled protobuf schema already present in the firmware repository
from clients.state_pb import state_pb2

DEVICE_IP = "192.168.68.196" # <-- Replace with your Busy Bar's IP

async def listen_to_busy_bar():
    uri = f"ws://{DEVICE_IP}/api/status/ws"

    print(f"Connecting to {uri}...")
    async with websockets.connect(uri) as websocket:
        print("Connected! Enabling stream...")

        # Send the command to turn on the WebSocket stream
        await websocket.send(json.dumps({"enable": True}))

        print("Listening for button presses... (Press the physical button!)")
        while True:
            # Receive the binary Protobuf frame
            message = await websocket.recv()

            if isinstance(message, bytes):
                # Decode the binary data into a Python object using the schema
                state = state_pb2.State()
                state.ParseFromString(message)

                for update in state.updates:
                    if update.HasField("input"):
                        input_event = update.input
                        from clients.state_pb import input_pb2
                        
                        action_name = input_pb2.ButtonAction.Name(input_event.action)
                        button_name = input_pb2.Button.Name(input_event.button)

                        print(f"Event: {button_name} was {action_name}")

                        if input_event.button == input_pb2.OK and input_event.action == input_pb2.SHORT:
                            print("👉 MAIN BUTTON TRIGGERED! Do something in Home Assistant here!")

if __name__ == "__main__":
    asyncio.run(listen_to_busy_bar())
