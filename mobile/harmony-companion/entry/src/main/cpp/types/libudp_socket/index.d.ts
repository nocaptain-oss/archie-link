interface UdpSocketModule {
  createUdpSocket(): number;
  udpSendTo(fd: number, data: ArrayBuffer, host: string, port: number): number;
  udpRecvFrom(fd: number, size: number, timeoutMs: number): Object | null;
  closeUdpSocket(fd: number): void;
  startRecvThread(
    fd: number,
    callback: (data: ArrayBuffer, address: string, port: number) => void
  ): boolean;
  stopRecvThread(): void;
}

declare const udpSocket: UdpSocketModule;
export default udpSocket;
