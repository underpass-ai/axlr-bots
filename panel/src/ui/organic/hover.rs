use std::time::{Duration, Instant};

use super::ember::Ember;
use super::{Motion, Shine, Signal, Surface};

/// Which surface glows and how much, as the pointer moves: the state
/// behind the organic backgrounds. It has no clock: the surfaces' signals
/// and the frames say what time it is. The glow of the surface under the
/// pointer fades in; the glow of one the pointer left fades out where it
/// was; a pointer that has not moved for a while lets its glow go to
/// sleep, so a panel nobody touches draws nothing.
#[derive(Debug, Clone, PartialEq)]
pub struct Hover {
    motion: Motion,
    /// The surface under the pointer.
    lit: Option<Ember>,
    /// The pointer rested on it so long that its glow went out; it comes
    /// back when the pointer moves.
    asleep: bool,
    /// A surface around the lit one that has the pointer as well: it
    /// glows as soon as the pointer leaves the one inside.
    around: Option<Ember>,
    /// The surfaces the pointer left, still fading out.
    trail: Vec<Ember>,
    /// The first moment it heard of: the waves' time counts from it.
    start: Option<Instant>,
    /// The latest moment it heard of.
    now: Option<Instant>,
    frame: u64,
}

impl Hover {
    /// The glow takes this long to come.
    pub const FADE_IN: Duration = Duration::from_millis(250);
    /// And this long to go.
    pub const FADE_OUT: Duration = Duration::from_millis(450);
    /// The time between two frames: twenty a second, enough for waves
    /// this slow and a third less work than thirty.
    pub const FRAME: Duration = Duration::from_millis(50);
    /// A pointer that has not moved for this long lets its glow go out.
    pub const REST_AFTER: Duration = Duration::from_secs(3);
    /// A surface that was heard this short ago still has the pointer: a
    /// bigger one around it does not take the glow from it.
    pub const HOLD: Duration = Duration::from_millis(250);
    /// The waves repeat after this many seconds, so their time can wrap
    /// and stay exact however long the panel is open.
    const LOOP: f64 = std::f64::consts::TAU / 0.05;

    pub fn new(motion: Motion) -> Self {
        Self {
            motion,
            lit: None,
            asleep: false,
            around: None,
            trail: Vec::new(),
            start: None,
            now: None,
            frame: 0,
        }
    }

    pub fn motion(&self) -> Motion {
        self.motion
    }

    /// Turns the backgrounds on or off. Off, every glow goes at once.
    pub fn set_motion(&mut self, motion: Motion) {
        self.motion = motion;
        if !motion.is_on() {
            self.lit = None;
            self.asleep = false;
            self.around = None;
            self.trail.clear();
        }
    }

    /// Whether the surfaces should tell about the pointer at all.
    pub fn listens(&self) -> bool {
        self.motion.is_on()
    }

    /// Whether a glow is changing: ask for frames while it is, and only
    /// then.
    pub fn is_animating(&self) -> bool {
        self.motion.is_on() && (!self.trail.is_empty() || self.lit.is_some_and(|lit| !(self.asleep && lit.is_out())))
    }

    /// What a surface says about the pointer.
    pub fn signal(&mut self, signal: Signal) {
        if !self.motion.is_on() {
            return;
        }
        match signal {
            Signal::Over {
                surface,
                at,
                size,
                when,
            } => {
                self.advance(when);
                let claim = Ember {
                    surface,
                    pointer: at,
                    area: size.width * size.height,
                    level: 0.0,
                    heard: when,
                };
                match &mut self.lit {
                    Some(lit) if lit.surface == surface => {
                        *lit = Ember {
                            level: lit.level,
                            ..claim
                        };
                        self.asleep = false;
                    }
                    // The pointer is on a smaller surface inside this one.
                    Some(lit) if lit.area < claim.area && when.saturating_duration_since(lit.heard) < Self::HOLD => {
                        self.around = Some(claim);
                    }
                    _ => self.light(claim),
                }
            }
            Signal::Left { surface, when } => {
                self.advance(when);
                if self.around.is_some_and(|around| around.surface == surface) {
                    self.around = None;
                }
                if self.lit.is_some_and(|lit| lit.surface == surface) {
                    let around = self.around.take();
                    self.release();
                    if let Some(around) = around
                        && when.saturating_duration_since(around.heard) < Self::HOLD
                    {
                        self.light(around);
                    }
                }
            }
        }
    }

    /// A frame: the glows move on to this moment and there is a new
    /// picture to draw.
    pub fn tick(&mut self, now: Instant) {
        if !self.motion.is_on() {
            return;
        }
        self.advance(now);
        self.frame += 1;
    }

    /// Lets go of the surface under the pointer: its glow fades out. For
    /// when the screen changes under a pointer that does not move, and
    /// the surface it was on is gone without saying so.
    pub fn release(&mut self) {
        self.around = None;
        self.asleep = false;
        if let Some(lit) = self.lit.take()
            && !lit.is_out()
        {
            self.trail.push(lit);
        }
    }

    /// What this surface draws now, if it glows.
    pub fn shine(&self, surface: Surface) -> Option<Shine> {
        if !self.motion.is_on() {
            return None;
        }
        let ember = self
            .lit
            .iter()
            .chain(&self.trail)
            .find(|ember| ember.surface == surface && !ember.is_out())?;
        Some(Shine {
            pointer: ember.pointer,
            fade: ember.eased(),
            time: self.time(),
            frame: self.frame,
        })
    }

    /// The waves' moment, in seconds.
    fn time(&self) -> f32 {
        let passed = match (self.start, self.now) {
            (Some(start), Some(now)) => now.saturating_duration_since(start),
            _ => Duration::ZERO,
        };
        (passed.as_secs_f64() % Self::LOOP) as f32
    }

    /// The glow passes to this surface; the one that had it fades out. A
    /// surface the pointer comes back to goes on from where its glow was.
    fn light(&mut self, claim: Ember) {
        self.release();
        let level = match self.trail.iter().position(|ember| ember.surface == claim.surface) {
            Some(at) => self.trail.remove(at).level,
            None => 0.0,
        };
        self.lit = Some(Ember { level, ..claim });
    }

    /// Moves every glow on to this moment.
    fn advance(&mut self, now: Instant) {
        let before = *self.now.get_or_insert(now);
        let _ = self.start.get_or_insert(now);
        let passed = now.saturating_duration_since(before);
        self.now = Some(before.max(now));
        if let Some(lit) = &mut self.lit {
            if now.saturating_duration_since(lit.heard) >= Self::REST_AFTER {
                self.asleep = true;
            }
            if self.asleep {
                lit.dim(passed, Self::FADE_OUT);
            } else {
                lit.brighten(passed, Self::FADE_IN);
            }
        }
        for ember in &mut self.trail {
            ember.dim(passed, Self::FADE_OUT);
        }
        self.trail.retain(|ember| !ember.is_out());
    }
}

impl Default for Hover {
    fn default() -> Self {
        Self::new(Motion::default())
    }
}

#[cfg(test)]
mod tests {
    use iced::{Point, Size};

    use super::*;

    const ROW: Size = Size::new(1200.0, 36.0);
    const CARD: Size = Size::new(420.0, 300.0);
    const TILE: Size = Size::new(140.0, 24.0);

    fn surface(name: &str) -> Surface {
        Surface::of(name)
    }

    fn ms(millis: u64) -> Duration {
        Duration::from_millis(millis)
    }

    fn over(name: &str, size: Size, when: Instant) -> Signal {
        Signal::Over {
            surface: surface(name),
            at: Point::new(0.5, 0.5),
            size,
            when,
        }
    }

    fn left(name: &str, when: Instant) -> Signal {
        Signal::Left {
            surface: surface(name),
            when,
        }
    }

    /// A frame every 34 ms from `from` until `until`, both from `start`.
    fn run(hover: &mut Hover, start: Instant, from: u64, until: u64) {
        let mut at = from;
        while at <= until {
            hover.tick(start + ms(at));
            at += Hover::FRAME.as_millis() as u64;
        }
        hover.tick(start + ms(until));
    }

    fn fade(hover: &Hover, name: &str) -> f32 {
        hover.shine(surface(name)).map_or(0.0, |shine| shine.fade)
    }

    #[test]
    fn nothing_glows_and_no_frame_is_asked_for_until_the_pointer_is_on_a_surface() {
        let mut hover = Hover::default();
        assert!(hover.listens() && !hover.is_animating());
        assert_eq!(hover.shine(surface("a")), None);
        hover.tick(Instant::now());
        assert!(!hover.is_animating());
    }

    #[test]
    fn the_glow_fades_in_in_a_quarter_of_a_second() {
        let start = Instant::now();
        let mut hover = Hover::default();
        hover.signal(over("a", ROW, start));
        assert!(hover.is_animating());
        assert_eq!(hover.shine(surface("a")), None, "nothing shows at the very start");
        hover.tick(start + ms(125));
        assert!(
            (fade(&hover, "a") - 0.5).abs() < 0.01,
            "half way: {}",
            fade(&hover, "a")
        );
        hover.tick(start + ms(200));
        assert!(fade(&hover, "a") > 0.85 && fade(&hover, "a") < 1.0);
        hover.tick(start + ms(251));
        assert_eq!(fade(&hover, "a"), 1.0);
        hover.tick(start + ms(900));
        assert_eq!(fade(&hover, "a"), 1.0);
        assert!(hover.is_animating(), "the waves go on while the pointer is there");
    }

    #[test]
    fn the_glow_fades_out_in_under_half_a_second_and_the_frames_stop() {
        let start = Instant::now();
        let mut hover = Hover::default();
        hover.signal(over("a", ROW, start));
        run(&mut hover, start, 34, 400);
        hover.signal(left("a", start + ms(400)));
        assert_eq!(fade(&hover, "a"), 1.0);
        hover.tick(start + ms(625));
        assert!(
            (fade(&hover, "a") - 0.5).abs() < 0.01,
            "half way out: {}",
            fade(&hover, "a")
        );
        assert!(hover.is_animating());
        hover.tick(start + ms(840));
        assert!(fade(&hover, "a") > 0.0 && fade(&hover, "a") < 0.01);
        hover.tick(start + ms(851));
        assert_eq!(hover.shine(surface("a")), None);
        assert!(!hover.is_animating());
    }

    #[test]
    fn the_glow_follows_the_pointer() {
        let start = Instant::now();
        let mut hover = Hover::default();
        hover.signal(over("a", ROW, start));
        hover.signal(Signal::Over {
            surface: surface("a"),
            at: Point::new(0.9, 0.2),
            size: ROW,
            when: start + ms(100),
        });
        hover.tick(start + ms(134));
        let shine = hover.shine(surface("a")).unwrap();
        assert_eq!(shine.pointer, Point::new(0.9, 0.2));
        assert!(shine.fade > 0.0, "moving does not start the fade again");
    }

    #[test]
    fn a_frame_is_a_new_picture_and_the_waves_move_on() {
        let start = Instant::now();
        let mut hover = Hover::default();
        hover.signal(over("a", ROW, start));
        hover.tick(start + ms(34));
        let first = hover.shine(surface("a")).unwrap();
        hover.signal(over("a", ROW, start + ms(40)));
        assert_eq!(
            hover.shine(surface("a")).unwrap().frame,
            first.frame,
            "a move is not a frame"
        );
        hover.tick(start + ms(68));
        let second = hover.shine(surface("a")).unwrap();
        assert_eq!(second.frame, first.frame + 1);
        assert!((second.time - 0.068).abs() < 0.001 && second.time > first.time);
    }

    #[test]
    fn the_waves_time_wraps_where_they_repeat() {
        let start = Instant::now();
        let mut hover = Hover::default();
        hover.signal(over("a", ROW, start));
        let days = start + Duration::from_secs(40 * 24 * 3600);
        hover.signal(over("a", ROW, days));
        hover.tick(days + ms(34));
        let time = hover.shine(surface("a")).unwrap().time;
        assert!((0.0..126.0).contains(&time), "{time}");
        // The waves are where they would be without the wrap.
        let whole = (40.0 * 24.0 * 3600.0 + 0.034) * 0.05_f64;
        let wrapped = f64::from(time) * 0.05;
        let turns = (whole - wrapped) / std::f64::consts::TAU;
        assert!((turns - turns.round()).abs() < 1e-3, "{turns}");
    }

    #[test]
    fn from_one_row_to_the_next_one_fades_out_as_the_other_fades_in() {
        let start = Instant::now();
        let mut hover = Hover::default();
        hover.signal(over("a", ROW, start));
        run(&mut hover, start, 34, 300);
        hover.signal(left("a", start + ms(300)));
        hover.signal(over("b", ROW, start + ms(300)));
        hover.tick(start + ms(425));
        assert!((fade(&hover, "b") - 0.5).abs() < 0.01);
        assert!(fade(&hover, "a") > 0.5 && fade(&hover, "a") < 1.0);
        run(&mut hover, start, 459, 760);
        assert_eq!(fade(&hover, "b"), 1.0);
        assert_eq!(hover.shine(surface("a")), None);
    }

    /// The row the pointer enters may tell before the row it left.
    #[test]
    fn it_does_not_matter_which_of_two_rows_speaks_first() {
        let start = Instant::now();
        let mut hover = Hover::default();
        hover.signal(over("a", ROW, start));
        run(&mut hover, start, 34, 300);
        hover.signal(over("b", ROW, start + ms(300)));
        hover.signal(left("a", start + ms(300)));
        run(&mut hover, start, 334, 1000);
        assert_eq!(fade(&hover, "b"), 1.0);
        assert_eq!(hover.shine(surface("a")), None);
        assert!(hover.is_animating());
    }

    #[test]
    fn a_surface_the_pointer_comes_back_to_goes_on_from_where_it_was() {
        let start = Instant::now();
        let mut hover = Hover::default();
        hover.signal(over("a", ROW, start));
        run(&mut hover, start, 34, 300);
        hover.signal(left("a", start + ms(300)));
        hover.tick(start + ms(390));
        let dimmed = fade(&hover, "a");
        assert!(dimmed > 0.5 && dimmed < 1.0);
        hover.signal(over("a", ROW, start + ms(390)));
        assert_eq!(fade(&hover, "a"), dimmed);
        hover.tick(start + ms(424));
        assert!(fade(&hover, "a") > dimmed);
    }

    /// A tile in a card: both have the pointer, the tile glows.
    #[test]
    fn of_two_surfaces_under_the_pointer_the_one_inside_glows() {
        let start = Instant::now();
        let mut hover = Hover::default();
        // The pointer comes onto the card, then onto a tile in it.
        hover.signal(over("card", CARD, start));
        run(&mut hover, start, 34, 300);
        hover.signal(over("tile", TILE, start + ms(300)));
        hover.signal(over("card", CARD, start + ms(300)));
        run(&mut hover, start, 334, 1000);
        hover.signal(over("tile", TILE, start + ms(1000)));
        hover.signal(over("card", CARD, start + ms(1000)));
        assert_eq!(fade(&hover, "tile"), 1.0);
        assert_eq!(hover.shine(surface("card")), None, "the card's glow went out");
        // It goes back to the card's own ground: the card glows at once.
        hover.signal(left("tile", start + ms(1010)));
        hover.tick(start + ms(1044));
        assert!(fade(&hover, "card") > 0.0);
        assert!(fade(&hover, "tile") < 1.0);
    }

    /// A row that goes away under the pointer (the list was filtered)
    /// never says the pointer left.
    #[test]
    fn a_surface_that_went_away_does_not_keep_the_glow_from_the_one_around_it() {
        let start = Instant::now();
        let mut hover = Hover::default();
        hover.signal(over("tile", TILE, start));
        hover.signal(over("card", CARD, start));
        run(&mut hover, start, 34, 400);
        hover.signal(over("tile", TILE, start + ms(400)));
        hover.signal(over("card", CARD, start + ms(400)));
        // The tile is gone; only the card speaks now.
        hover.signal(over("card", CARD, start + ms(420)));
        hover.tick(start + ms(454));
        assert_eq!(hover.shine(surface("card")), None, "the tile was just heard");
        hover.signal(over("card", CARD, start + ms(700)));
        hover.tick(start + ms(734));
        assert!(fade(&hover, "card") > 0.0);
        assert!(fade(&hover, "tile") < 1.0);
    }

    #[test]
    fn releasing_fades_the_glow_out_and_the_frames_stop() {
        let start = Instant::now();
        let mut hover = Hover::default();
        hover.signal(over("a", ROW, start));
        run(&mut hover, start, 34, 300);
        hover.release();
        assert!(hover.is_animating());
        assert_eq!(fade(&hover, "a"), 1.0);
        run(&mut hover, start, 334, 760);
        assert_eq!(hover.shine(surface("a")), None);
        assert!(!hover.is_animating());
        // Late word from the surface that is gone changes nothing.
        hover.signal(left("a", start + ms(800)));
        assert!(!hover.is_animating());
    }

    #[test]
    fn a_pointer_that_rests_lets_the_glow_go_out_and_wakes_it_when_it_moves() {
        let start = Instant::now();
        let mut hover = Hover::default();
        hover.signal(over("a", ROW, start));
        let rest = Hover::REST_AFTER.as_millis() as u64;
        run(&mut hover, start, 50, rest - 10);
        assert_eq!(fade(&hover, "a"), 1.0);
        assert!(hover.is_animating());
        run(&mut hover, start, rest, rest + 500);
        assert_eq!(hover.shine(surface("a")), None);
        assert!(!hover.is_animating(), "a panel nobody touches asks for no frames");
        hover.signal(over("a", ROW, start + ms(60_000)));
        assert!(hover.is_animating());
        hover.tick(start + ms(60_125));
        assert!((fade(&hover, "a") - 0.5).abs() < 0.01);
    }

    /// A glow takes time to come and goes in less than twice that, so
    /// a pointer that runs down a list leaves a short tail, not a list
    /// of glows.
    #[test]
    fn a_pointer_that_runs_down_a_list_leaves_a_short_tail() {
        let start = Instant::now();
        let mut hover = Hover::default();
        let names = ["a", "b", "c", "d", "e", "f", "g", "h", "i", "j"];
        for (row, name) in names.iter().enumerate() {
            let at = start + ms(row as u64 * 60);
            hover.signal(over(name, ROW, at));
            hover.tick(at + ms(34));
            hover.tick(at + ms(59));
        }
        let glowing = names.iter().filter(|name| hover.shine(surface(name)).is_some()).count();
        assert!((2..=3).contains(&glowing), "{glowing} glow");
        assert!(hover.shine(surface("j")).is_some());
    }

    #[test]
    fn with_motion_off_nothing_listens_glows_or_asks_for_frames() {
        let start = Instant::now();
        let mut hover = Hover::new(Motion::Off);
        assert!(!hover.listens());
        hover.signal(over("a", ROW, start));
        hover.tick(start + ms(250));
        assert_eq!(hover.shine(surface("a")), None);
        assert!(!hover.is_animating());

        // Turned off while a surface glows: it goes at once.
        let mut hover = Hover::default();
        hover.signal(over("a", ROW, start));
        hover.signal(left("b", start));
        run(&mut hover, start, 34, 300);
        assert_eq!(fade(&hover, "a"), 1.0);
        hover.set_motion(Motion::Off);
        assert_eq!(hover.motion(), Motion::Off);
        assert_eq!(hover.shine(surface("a")), None);
        assert!(!hover.is_animating());

        // And on again: the next move of the pointer lights it.
        hover.set_motion(Motion::On);
        assert!(!hover.is_animating());
        hover.signal(over("a", ROW, start + ms(2_000)));
        hover.tick(start + ms(2_250));
        assert_eq!(fade(&hover, "a"), 1.0);
    }
}
