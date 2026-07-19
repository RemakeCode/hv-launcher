import { Spinner } from "@decky/ui";

// language=css
const spinnerStyles = `
  .hv-launcher-spinner-wrapper {
    display: flex;
    height: 70vh;
    width: 100%;
    align-items: center;
    justify-content: center;
  }
`;

export function LoadingSpinner() {
  return (
    <>
      <style>{spinnerStyles}</style>
      <div className="hv-launcher-spinner-wrapper">
        <Spinner width="24px" height="24px" />
      </div>
    </>
  );
}
