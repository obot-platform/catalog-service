import { BrowserRouter as Router, Routes, Route } from "react-router-dom";
import Header from "./components/Header";
import Dashboard from "./components/Dashboard";
import RepositoryDetail from "./components/RepositoryDetail";
import RepositoryList from "./components/RepositoryList";
import { Toaster } from "./components/ui/toaster";
import "./index.css";

function App() {
  return (
    <Router>
      <div className="min-h-screen bg-background font-sans antialiased">
        <Header />
        <main>
          <Routes>
            <Route path="/" element={<Dashboard />} />
            <Route path="/repositories" element={<RepositoryList />} />
            <Route path="/repository/:id" element={<RepositoryDetail />} />
          </Routes>
          <Toaster />
        </main>
      </div>
    </Router>
  );
}

export default App;
