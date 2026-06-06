export function Footer() {
  return (
    <footer className="footer">
      <div className="container footer__inner">
        <div className="footer__copy">
          © {new Date().getFullYear()} 91
        </div>
      </div>
    </footer>
  );
}
